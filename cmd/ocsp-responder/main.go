package notmain

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha1"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-gorp/gorp/v3"
	"github.com/honeycombio/beeline-go"
	"github.com/honeycombio/beeline-go/wrappers/hnynethttp"
	"github.com/jmhodges/clock"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/crypto/ocsp"

	"github.com/letsencrypt/boulder/cmd"
	"github.com/letsencrypt/boulder/core"
	"github.com/letsencrypt/boulder/db"
	"github.com/letsencrypt/boulder/features"
	"github.com/letsencrypt/boulder/issuance"
	blog "github.com/letsencrypt/boulder/log"
	"github.com/letsencrypt/boulder/metrics/measured_http"
	bocsp "github.com/letsencrypt/boulder/ocsp"
	"github.com/letsencrypt/boulder/rocsp"
	rocsp_config "github.com/letsencrypt/boulder/rocsp/config"
	"github.com/letsencrypt/boulder/sa"
	"github.com/letsencrypt/boulder/test/ocsp/helper"
)

// ocspFilter stores information needed to filter OCSP requests (to ensure we
// aren't trying to serve OCSP for certs which aren't ours), and surfaces
// methods to determine if a given request should be filtered or not.
type ocspFilter struct {
	issuerKeyHashAlgorithm crypto.Hash
	// TODO(#5152): Simplify this when we've fully deprecated old-style IssuerIDs.
	issuerKeyHashes     map[issuance.IssuerID][]byte
	issuerNameKeyHashes map[issuance.IssuerNameID][]byte
	serialPrefixes      []string
}

// newFilter creates a new ocspFilter which will accept a request only if it
// uses the SHA1 algorithm to hash the issuer key, the issuer key matches one
// of the given issuer certs (here, paths to PEM certs on disk), and the serial
// has one of the given prefixes.
func newFilter(issuerCerts []string, serialPrefixes []string) (*ocspFilter, error) {
	if len(issuerCerts) < 1 {
		return nil, errors.New("Filter must include at least 1 issuer cert")
	}
	issuerKeyHashes := make(map[issuance.IssuerID][]byte, 0)
	issuerNameKeyHashes := make(map[issuance.IssuerNameID][]byte, 0)
	for _, issuerCert := range issuerCerts {
		// Load the certificate from the file path.
		cert, err := core.LoadCert(issuerCert)
		if err != nil {
			return nil, fmt.Errorf("Could not load issuer cert %s: %w", issuerCert, err)
		}
		caCert := &issuance.Certificate{Certificate: cert}
		// The issuerKeyHash in OCSP requests is constructed over the DER
		// encoding of the public key per RFC 6960 (defined in RFC 4055 for
		// RSA and RFC 5480 for ECDSA). We can't use MarshalPKIXPublicKey
		// for this since it encodes keys using the SPKI structure itself,
		// and we just want the contents of the subjectPublicKey for the
		// hash, so we need to extract it ourselves.
		var spki struct {
			Algo      pkix.AlgorithmIdentifier
			BitString asn1.BitString
		}
		if _, err := asn1.Unmarshal(caCert.RawSubjectPublicKeyInfo, &spki); err != nil {
			return nil, err
		}
		keyHash := sha1.Sum(spki.BitString.Bytes)
		issuerKeyHashes[caCert.ID()] = keyHash[:]
		issuerNameKeyHashes[caCert.NameID()] = keyHash[:]
	}
	return &ocspFilter{crypto.SHA1, issuerKeyHashes, issuerNameKeyHashes, serialPrefixes}, nil
}

type Responder struct {
	clk         clock.Clock
	log         blog.Logger
	timeout     time.Duration
	ocspLookups *prometheus.CounterVec
	sourceUsed  *prometheus.CounterVec
}

func New(
	stats prometheus.Registerer,
	clk clock.Clock,
	log blog.Logger,
	c config,
) (*Responder, error) {
	// Metrics for response lookups
	ocspLookups := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ocsp_lookups",
		Help: "A counter of ocsp lookups labeled by source_result",
	}, []string{"result"})
	stats.MustRegister(ocspLookups)

	sourceUsed := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "lookup_source_used",
		Help: "A counter of lookups returned labeled by source used",
	}, []string{"source"})
	stats.MustRegister(sourceUsed)

	responder := Responder{
		clk:         clk,
		log:         log,
		timeout:     c.OCSPResponder.Timeout.Duration,
		ocspLookups: ocspLookups,
		sourceUsed:  sourceUsed,
	}
	return &responder, nil
}

// checkRequest returns a descriptive error if the request does not satisfy any of
// the requirements of an OCSP request, or nil if the request should be handled.
func (f *ocspFilter) checkRequest(req *ocsp.Request) error {
	if req.HashAlgorithm != f.issuerKeyHashAlgorithm {
		return fmt.Errorf("Request ca key hash using unsupported algorithm %s: %w", req.HashAlgorithm, bocsp.ErrNotFound)
	}
	// Check that this request is for the proper CA. We only iterate over
	// issuerKeyHashes here because it is guaranteed to have the same values
	// as issuerNameKeyHashes.
	match := false
	for _, keyHash := range f.issuerKeyHashes {
		if match = bytes.Equal(req.IssuerKeyHash, keyHash); match {
			break
		}
	}
	if !match {
		return fmt.Errorf("Request intended for wrong issuer cert %s: %w", hex.EncodeToString(req.IssuerKeyHash), bocsp.ErrNotFound)
	}

	serialString := core.SerialToString(req.SerialNumber)
	if len(f.serialPrefixes) > 0 {
		match := false
		for _, prefix := range f.serialPrefixes {
			if match = strings.HasPrefix(serialString, prefix); match {
				break
			}
		}
		if !match {
			return fmt.Errorf("Request serial has wrong prefix: %w", bocsp.ErrNotFound)
		}
	}

	return nil
}

// responseMatchesIssuer returns true if the CertificateStatus (from the db)
// was generated by an issuer matching the key hash in the original request.
// This filters out, for example, responses which are for a serial that we
// issued, but from a different issuer than that contained in the request.
func (f *ocspFilter) responseMatchesIssuer(req *ocsp.Request, status core.CertificateStatus) bool {
	issuerKeyHash, ok := f.issuerNameKeyHashes[issuance.IssuerNameID(status.IssuerID)]
	if !ok {
		// TODO(#5152): Remove this fallback to old-style IssuerIDs.
		issuerKeyHash, ok = f.issuerKeyHashes[issuance.IssuerID(status.IssuerID)]
		if !ok {
			return false
		}
	}
	return bytes.Equal(issuerKeyHash, req.IssuerKeyHash)
}

// dbSource represents a database containing pre-generated OCSP responses keyed
// by serial number. It also allows for filtering requests by their issuer key
// hash and serial number, to prevent unnecessary lookups for rows that we know
// will not exist in the database.
//
// We assume that OCSP responses are stored in a very simple database table,
// with at least these two columns: serialNumber (TEXT) and response (BLOB).
//
// The serialNumber field may have any type to which Go will match a string,
// so you can be more efficient than TEXT if you like. We use it to store the
// serial number in hex. You must have an index on the serialNumber field,
// since we will always query on it.
type dbSource struct {
	primaryLookup   ocspLookup
	secondaryLookup ocspLookup
	filter          *ocspFilter
	*Responder
}

// Define an interface with the needed methods from gorp.
// This also allows us to simulate MySQL failures by mocking the interface.
type dbSelector interface {
	SelectOne(holder interface{}, query string, args ...interface{}) error
	WithContext(ctx context.Context) gorp.SqlExecutor
}

// Response is called by the HTTP server to handle a new OCSP request.
func (src *dbSource) Response(ctx context.Context, req *ocsp.Request) ([]byte, http.Header, error) {
	err := src.filter.checkRequest(req)
	if err != nil {
		src.log.Debugf("Not responding to filtered OCSP request: %s", err.Error())
		return nil, nil, err
	}

	serialString := core.SerialToString(req.SerialNumber)
	src.log.Debugf("Searching for OCSP issued by us for serial %s", serialString)

	var header http.Header = make(map[string][]string)
	if len(serialString) > 2 {
		// Set a cache tag that is equal to the last two bytes of the serial.
		// We expect that to be randomly distributed, so each tag should map to
		// about 1/256 of our responses.
		header.Add("Edge-Cache-Tag", serialString[len(serialString)-2:])
	}

	var certStatus core.CertificateStatus
	defer func() {
		if len(certStatus.OCSPResponse) != 0 {
			src.log.Debugf("OCSP Response sent for CA=%s, Serial=%s", hex.EncodeToString(req.IssuerKeyHash), serialString)
		}
	}()
	if src.timeout != 0 {
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, src.timeout)
		defer cancel()
	}

	// The primary and secondary lookups send goroutines to get an OCSP
	// status given a serial and return a channel of the output.
	primaryChan := src.primaryLookup.getResponse(ctx, req)

	// If the redis source is nil, don't try to get a response.
	var secondaryChan chan lookupResponse
	if src.secondaryLookup != nil {
		secondaryChan = src.secondaryLookup.getResponse(ctx, req)
	}

	// If the primary source returns first, check the output and return
	// it. If the secondary source wins, then wait for the primary so the
	// results from the secondary can be verified. It is important that we
	// never return a response from the redis source that is good if mysql
	// has a revoked status. If the secondary source wins the race and
	// passes these checks, return its response instead.
	select {
	case <-ctx.Done():
		err := fmt.Errorf("looking up OCSP response for serial: %s err: %w", serialString, ctx.Err())
		src.log.Debugf(err.Error())
		src.ocspLookups.WithLabelValues("canceled").Inc()
		return nil, nil, err
	case primaryResult := <-primaryChan:
		if primaryResult.err != nil {
			src.log.AuditErrf("Looking up OCSP response: %s", err)
			src.ocspLookups.WithLabelValues("mysql_failed").Inc()
			src.sourceUsed.WithLabelValues("error_returned").Inc()
			return nil, nil, primaryResult.err
		}
		// Parse the OCSP bytes returned from the primary source to check
		// status, expiration and other fields.
		primaryParsed, err := ocsp.ParseResponse(primaryResult.bytes, nil)
		if err != nil {
			src.log.AuditErrf("parsing OCSP response: %s", err)
			src.ocspLookups.WithLabelValues("mysql_failed").Inc()
			src.sourceUsed.WithLabelValues("error_returned").Inc()
			return nil, nil, err
		}
		src.log.Debugf("returning ocsp from primary source: %v", helper.PrettyResponse(primaryParsed))
		src.ocspLookups.WithLabelValues("mysql_success").Inc()
		src.sourceUsed.WithLabelValues("mysql").Inc()
		return primaryResult.bytes, header, nil
	case secondaryResult := <-secondaryChan:
		// If secondary returns first, wait for primary to return for
		// comparison.
		var primaryResult lookupResponse
		// Listen for cancellation or timeout waiting for primary result.
		select {
		case <-ctx.Done():
			err := fmt.Errorf("looking up OCSP response for serial: %s err: %w", serialString, ctx.Err())
			src.log.Debugf(err.Error())
			src.ocspLookups.WithLabelValues("canceled").Inc()
			return nil, nil, err
		case primaryResult = <-primaryChan:
		}

		if primaryResult.err != nil {
			src.log.AuditErrf("Looking up OCSP response: %s", err)
			src.ocspLookups.WithLabelValues("mysql_failed").Inc()
			src.sourceUsed.WithLabelValues("error_returned").Inc()
			return nil, nil, primaryResult.err
		}
		// Parse the OCSP bytes returned from the primary source to check
		// status, expiration and other fields.
		primaryParsed, err := ocsp.ParseResponse(primaryResult.bytes, nil)
		if err != nil {
			src.log.AuditErrf("parsing OCSP response: %s", err)
			src.ocspLookups.WithLabelValues("mysql_failed").Inc()
			src.sourceUsed.WithLabelValues("error_returned").Inc()
			return nil, nil, err
		}

		secondaryParsed, err := ocsp.ParseResponse(secondaryResult.bytes, nil)
		if err != nil {
			src.log.Debugf("secondary OCSP lookup response error: %v", err)
			src.ocspLookups.WithLabelValues("redis_failed").Inc()
			src.sourceUsed.WithLabelValues("mysql").Inc()
			return primaryResult.bytes, header, nil
		}
		if primaryParsed.Status != secondaryParsed.Status {
			src.log.Err("primary ocsp source doesn't match secondary source, returning primary response")
			src.ocspLookups.WithLabelValues("redis_mismatch").Inc()
			src.sourceUsed.WithLabelValues("mysql").Inc()
			return primaryResult.bytes, header, nil
		}
		src.log.Debugf("returning ocsp from secondary source: %v", helper.PrettyResponse(secondaryParsed))
		src.ocspLookups.WithLabelValues("redis_success").Inc()
		src.sourceUsed.WithLabelValues("redis").Inc()
		return secondaryResult.bytes, header, nil
	}
}

type ocspLookup interface {
	getResponse(context.Context, *ocsp.Request) chan lookupResponse
}

type redisReceiver struct {
	rocspReader *rocsp.Client
}
type dbReceiver struct {
	dbMap  dbSelector
	filter *ocspFilter
	*Responder
}

type lookupResponse struct {
	bytes []byte
	err   error
}

func (src dbReceiver) getResponse(ctx context.Context, req *ocsp.Request) chan lookupResponse {
	responseChan := make(chan lookupResponse)
	serialString := core.SerialToString(req.SerialNumber)

	go func() {
		defer close(responseChan)
		certStatus, err := sa.SelectCertificateStatus(src.dbMap.WithContext(ctx), serialString)
		if err != nil {
			if db.IsNoRows(err) {
				responseChan <- lookupResponse{nil, bocsp.ErrNotFound}
				return
			}
			responseChan <- lookupResponse{nil, err}
		}

		if certStatus.IsExpired {
			src.log.Infof("OCSP Response not sent (expired) for CA=%s, Serial=%s", hex.EncodeToString(req.IssuerKeyHash), serialString)
			responseChan <- lookupResponse{nil, bocsp.ErrNotFound}
			return
		} else if certStatus.OCSPLastUpdated.IsZero() {
			src.log.Warningf("OCSP Response not sent (ocspLastUpdated is zero) for CA=%s, Serial=%s", hex.EncodeToString(req.IssuerKeyHash), serialString)
			responseChan <- lookupResponse{nil, bocsp.ErrNotFound}
			return
		} else if !src.filter.responseMatchesIssuer(req, certStatus) {
			src.log.Warningf("OCSP Response not sent (issuer and serial mismatch) for CA=%s, Serial=%s", hex.EncodeToString(req.IssuerKeyHash), serialString)
			responseChan <- lookupResponse{nil, bocsp.ErrNotFound}
			return
		}
		responseChan <- lookupResponse{certStatus.OCSPResponse, err}

	}()

	return responseChan
}

func (src redisReceiver) getResponse(ctx context.Context, req *ocsp.Request) chan lookupResponse {
	responseChan := make(chan lookupResponse)
	serialString := core.SerialToString(req.SerialNumber)

	go func() {
		defer close(responseChan)
		respBytes, err := src.rocspReader.GetResponse(ctx, serialString)
		responseChan <- lookupResponse{respBytes, err}
	}()

	return responseChan
}

type config struct {
	OCSPResponder struct {
		cmd.ServiceConfig
		DB cmd.DBConfig

		// Source indicates the source of pre-signed OCSP responses to be used. It
		// can be a DBConnect string or a file URL. The file URL style is used
		// when responding from a static file for intermediates and roots.
		// If DBConfig has non-empty fields, it takes precedence over this.
		Source string

		// The list of issuer certificates, against which OCSP requests/responses
		// are checked to ensure we're not responding for anyone else's certs.
		IssuerCerts []string

		Path          string
		ListenAddress string
		// MaxAge is the max-age to set in the Cache-Control response
		// header. It is a time.Duration formatted string.
		MaxAge cmd.ConfigDuration

		// When to timeout a request. This should be slightly lower than the
		// upstream's timeout when making request to ocsp-responder.
		Timeout cmd.ConfigDuration

		ShutdownStopTimeout cmd.ConfigDuration

		RequiredSerialPrefixes []string

		Features map[string]bool

		Redis rocsp_config.RedisConfig
	}

	Syslog  cmd.SyslogConfig
	Beeline cmd.BeelineConfig
}

func main() {
	configFile := flag.String("config", "", "File path to the configuration file for this service")
	flag.Parse()
	if *configFile == "" {
		fmt.Fprintf(os.Stderr, `Usage of %s:
Config JSON should contain either a DBConnectFile or a Source value containing a file: URL.
If Source is a file: URL, the file should contain a list of OCSP responses in base64-encoded DER,
as generated by Boulder's ceremony command.
`, os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	var c config
	err := cmd.ReadConfigFile(*configFile, &c)
	cmd.FailOnError(err, "Reading JSON config file into config structure")
	err = features.Set(c.OCSPResponder.Features)
	cmd.FailOnError(err, "Failed to set feature flags")

	clk := cmd.Clock()

	bc, err := c.Beeline.Load()
	cmd.FailOnError(err, "Failed to load Beeline config")
	beeline.Init(bc)
	defer beeline.Close()

	stats, logger := cmd.StatsAndLogging(c.Syslog, c.OCSPResponder.DebugAddr)
	defer logger.AuditPanic()
	logger.Info(cmd.VersionString())

	responder, err := New(stats, clk, logger, c)
	if err != nil {
		cmd.FailOnError(err, "Could not create OCSPResponder object")
	}

	config := c.OCSPResponder
	var source bocsp.Source

	if strings.HasPrefix(config.Source, "file:") {
		url, err := url.Parse(config.Source)
		cmd.FailOnError(err, "Source was not a URL")
		filename := url.Path
		// Go interprets cwd-relative file urls (file:test/foo.txt) as having the
		// relative part of the path in the 'Opaque' field.
		if filename == "" {
			filename = url.Opaque
		}
		source, err = bocsp.NewMemorySourceFromFile(filename, logger)
		cmd.FailOnError(err, fmt.Sprintf("Couldn't read file: %s", url.Path))
	} else {
		// For databases, DBConfig takes precedence over Source, if present.
		dbConnect, err := config.DB.URL()
		cmd.FailOnError(err, "Reading DB config")
		if dbConnect == "" {
			dbConnect = config.Source
		}
		dbSettings := sa.DbSettings{
			MaxOpenConns:    config.DB.MaxOpenConns,
			MaxIdleConns:    config.DB.MaxIdleConns,
			ConnMaxLifetime: config.DB.ConnMaxLifetime.Duration,
			ConnMaxIdleTime: config.DB.ConnMaxIdleTime.Duration,
		}
		dbMap, err := sa.NewDbMap(dbConnect, dbSettings)
		cmd.FailOnError(err, "Could not connect to database")
		sa.SetSQLDebug(dbMap, logger)

		dbAddr, dbUser, err := config.DB.DSNAddressAndUser()
		cmd.FailOnError(err, "Could not determine address or user of DB DSN")

		sa.InitDBMetrics(dbMap.Db, stats, dbSettings, dbAddr, dbUser)

		issuerCerts := c.OCSPResponder.IssuerCerts

		filter, err := newFilter(issuerCerts, c.OCSPResponder.RequiredSerialPrefixes)
		cmd.FailOnError(err, "Couldn't create OCSP filter")

		pLookup := dbReceiver{dbMap, filter, responder}

		// Set up the redis source if there is a config. Otherwise just
		// set up a mysql source.
		if c.OCSPResponder.Redis.Addrs != nil {
			logger.Info("redis config found, configuring redis reader")
			rocspReader, err := rocsp_config.MakeReadClient(&c.OCSPResponder.Redis, clk)
			if err != nil {
				cmd.FailOnError(err, "could not make redis client")
			}
			source = &dbSource{
				primaryLookup:   pLookup,
				secondaryLookup: redisReceiver{rocspReader},
				filter:          filter,
				Responder:       responder,
			}
		} else {
			logger.Info("no redis config found, using mysql as only ocsp source")
			source = &dbSource{
				primaryLookup:   pLookup,
				secondaryLookup: nil,
				filter:          filter,
				Responder:       responder,
			}

		}

		// Export the value for dbSettings.MaxOpenConns
		dbConnStat := prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "max_db_connections",
			Help: "Maximum number of DB connections allowed.",
		})
		stats.MustRegister(dbConnStat)
		dbConnStat.Set(float64(dbSettings.MaxOpenConns))
	}

	m := mux(stats, c.OCSPResponder.Path, source, logger)
	srv := &http.Server{
		Addr:    c.OCSPResponder.ListenAddress,
		Handler: m,
	}

	done := make(chan bool)
	go cmd.CatchSignals(logger, func() {
		ctx, cancel := context.WithTimeout(context.Background(),
			c.OCSPResponder.ShutdownStopTimeout.Duration)
		defer cancel()
		_ = srv.Shutdown(ctx)
		done <- true
	})

	err = srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		cmd.FailOnError(err, "Running HTTP server")
	}

	// https://godoc.org/net/http#Server.Shutdown:
	// When Shutdown is called, Serve, ListenAndServe, and ListenAndServeTLS
	// immediately return ErrServerClosed. Make sure the program doesn't exit and
	// waits instead for Shutdown to return.
	<-done
}

// ocspMux partially implements the interface defined for http.ServeMux but doesn't implement
// the path cleaning its Handler method does. Notably http.ServeMux will collapse repeated
// slashes into a single slash which breaks the base64 encoding that is used in OCSP GET
// requests. ocsp.Responder explicitly recommends against using http.ServeMux
// for this reason.
type ocspMux struct {
	handler http.Handler
}

func (om *ocspMux) Handler(_ *http.Request) (http.Handler, string) {
	return om.handler, "/"
}

func mux(stats prometheus.Registerer, responderPath string, source bocsp.Source, logger blog.Logger) http.Handler {
	stripPrefix := http.StripPrefix(responderPath, bocsp.NewResponder(source, stats, logger))
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "max-age=43200") // Cache for 12 hours
			w.WriteHeader(200)
			return
		}
		stripPrefix.ServeHTTP(w, r)
	})
	return hnynethttp.WrapHandler(measured_http.New(&ocspMux{h}, cmd.Clock(), stats))
}

func init() {
	cmd.RegisterCommand("ocsp-responder", main)
}
