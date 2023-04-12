// Code generated by smithy-go-codegen DO NOT EDIT.

package s3

import (
	"context"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	internalChecksum "github.com/aws/aws-sdk-go-v2/service/internal/checksum"
	s3cust "github.com/aws/aws-sdk-go-v2/service/s3/internal/customizations"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// Places an Object Lock configuration on the specified bucket. The rule specified
// in the Object Lock configuration will be applied by default to every new object
// placed in the specified bucket. For more information, see Locking Objects (https://docs.aws.amazon.com/AmazonS3/latest/dev/object-lock.html)
// .
//   - The DefaultRetention settings require both a mode and a period.
//   - The DefaultRetention period can be either Days or Years but you must select
//     one. You cannot specify Days and Years at the same time.
//   - You can only enable Object Lock for new buckets. If you want to turn on
//     Object Lock for an existing bucket, contact Amazon Web Services Support.
func (c *Client) PutObjectLockConfiguration(ctx context.Context, params *PutObjectLockConfigurationInput, optFns ...func(*Options)) (*PutObjectLockConfigurationOutput, error) {
	if params == nil {
		params = &PutObjectLockConfigurationInput{}
	}

	result, metadata, err := c.invokeOperation(ctx, "PutObjectLockConfiguration", params, optFns, c.addOperationPutObjectLockConfigurationMiddlewares)
	if err != nil {
		return nil, err
	}

	out := result.(*PutObjectLockConfigurationOutput)
	out.ResultMetadata = metadata
	return out, nil
}

type PutObjectLockConfigurationInput struct {

	// The bucket whose Object Lock configuration you want to create or replace.
	//
	// This member is required.
	Bucket *string

	// Indicates the algorithm used to create the checksum for the object when using
	// the SDK. This header will not provide any additional functionality if not using
	// the SDK. When sending this header, there must be a corresponding x-amz-checksum
	// or x-amz-trailer header sent. Otherwise, Amazon S3 fails the request with the
	// HTTP status code 400 Bad Request . For more information, see Checking object
	// integrity (https://docs.aws.amazon.com/AmazonS3/latest/userguide/checking-object-integrity.html)
	// in the Amazon S3 User Guide. If you provide an individual checksum, Amazon S3
	// ignores any provided ChecksumAlgorithm parameter.
	ChecksumAlgorithm types.ChecksumAlgorithm

	// The MD5 hash for the request body. For requests made using the Amazon Web
	// Services Command Line Interface (CLI) or Amazon Web Services SDKs, this field is
	// calculated automatically.
	ContentMD5 *string

	// The account ID of the expected bucket owner. If the bucket is owned by a
	// different account, the request fails with the HTTP status code 403 Forbidden
	// (access denied).
	ExpectedBucketOwner *string

	// The Object Lock configuration that you want to apply to the specified bucket.
	ObjectLockConfiguration *types.ObjectLockConfiguration

	// Confirms that the requester knows that they will be charged for the request.
	// Bucket owners need not specify this parameter in their requests. For information
	// about downloading objects from Requester Pays buckets, see Downloading Objects
	// in Requester Pays Buckets (https://docs.aws.amazon.com/AmazonS3/latest/dev/ObjectsinRequesterPaysBuckets.html)
	// in the Amazon S3 User Guide.
	RequestPayer types.RequestPayer

	// A token to allow Object Lock to be enabled for an existing bucket.
	Token *string

	noSmithyDocumentSerde
}

type PutObjectLockConfigurationOutput struct {

	// If present, indicates that the requester was successfully charged for the
	// request.
	RequestCharged types.RequestCharged

	// Metadata pertaining to the operation's result.
	ResultMetadata middleware.Metadata

	noSmithyDocumentSerde
}

func (c *Client) addOperationPutObjectLockConfigurationMiddlewares(stack *middleware.Stack, options Options) (err error) {
	err = stack.Serialize.Add(&awsRestxml_serializeOpPutObjectLockConfiguration{}, middleware.After)
	if err != nil {
		return err
	}
	err = stack.Deserialize.Add(&awsRestxml_deserializeOpPutObjectLockConfiguration{}, middleware.After)
	if err != nil {
		return err
	}
	if err = addSetLoggerMiddleware(stack, options); err != nil {
		return err
	}
	if err = awsmiddleware.AddClientRequestIDMiddleware(stack); err != nil {
		return err
	}
	if err = smithyhttp.AddComputeContentLengthMiddleware(stack); err != nil {
		return err
	}
	if err = addResolveEndpointMiddleware(stack, options); err != nil {
		return err
	}
	if err = v4.AddComputePayloadSHA256Middleware(stack); err != nil {
		return err
	}
	if err = addRetryMiddlewares(stack, options); err != nil {
		return err
	}
	if err = addHTTPSignerV4Middleware(stack, options); err != nil {
		return err
	}
	if err = awsmiddleware.AddRawResponseToMetadata(stack); err != nil {
		return err
	}
	if err = awsmiddleware.AddRecordResponseTiming(stack); err != nil {
		return err
	}
	if err = addClientUserAgent(stack); err != nil {
		return err
	}
	if err = smithyhttp.AddErrorCloseResponseBodyMiddleware(stack); err != nil {
		return err
	}
	if err = smithyhttp.AddCloseResponseBodyMiddleware(stack); err != nil {
		return err
	}
	if err = swapWithCustomHTTPSignerMiddleware(stack, options); err != nil {
		return err
	}
	if err = addOpPutObjectLockConfigurationValidationMiddleware(stack); err != nil {
		return err
	}
	if err = stack.Initialize.Add(newServiceMetadataMiddleware_opPutObjectLockConfiguration(options.Region), middleware.Before); err != nil {
		return err
	}
	if err = addMetadataRetrieverMiddleware(stack); err != nil {
		return err
	}
	if err = addPutObjectLockConfigurationInputChecksumMiddlewares(stack, options); err != nil {
		return err
	}
	if err = addPutObjectLockConfigurationUpdateEndpoint(stack, options); err != nil {
		return err
	}
	if err = addResponseErrorMiddleware(stack); err != nil {
		return err
	}
	if err = v4.AddContentSHA256HeaderMiddleware(stack); err != nil {
		return err
	}
	if err = disableAcceptEncodingGzip(stack); err != nil {
		return err
	}
	if err = addRequestResponseLogging(stack, options); err != nil {
		return err
	}
	return nil
}

func newServiceMetadataMiddleware_opPutObjectLockConfiguration(region string) *awsmiddleware.RegisterServiceMetadata {
	return &awsmiddleware.RegisterServiceMetadata{
		Region:        region,
		ServiceID:     ServiceID,
		SigningName:   "s3",
		OperationName: "PutObjectLockConfiguration",
	}
}

// getPutObjectLockConfigurationRequestAlgorithmMember gets the request checksum
// algorithm value provided as input.
func getPutObjectLockConfigurationRequestAlgorithmMember(input interface{}) (string, bool) {
	in := input.(*PutObjectLockConfigurationInput)
	if len(in.ChecksumAlgorithm) == 0 {
		return "", false
	}
	return string(in.ChecksumAlgorithm), true
}

func addPutObjectLockConfigurationInputChecksumMiddlewares(stack *middleware.Stack, options Options) error {
	return internalChecksum.AddInputMiddleware(stack, internalChecksum.InputMiddlewareOptions{
		GetAlgorithm:                     getPutObjectLockConfigurationRequestAlgorithmMember,
		RequireChecksum:                  true,
		EnableTrailingChecksum:           false,
		EnableComputeSHA256PayloadHash:   true,
		EnableDecodedContentLengthHeader: true,
	})
}

// getPutObjectLockConfigurationBucketMember returns a pointer to string denoting
// a provided bucket member valueand a boolean indicating if the input has a
// modeled bucket name,
func getPutObjectLockConfigurationBucketMember(input interface{}) (*string, bool) {
	in := input.(*PutObjectLockConfigurationInput)
	if in.Bucket == nil {
		return nil, false
	}
	return in.Bucket, true
}
func addPutObjectLockConfigurationUpdateEndpoint(stack *middleware.Stack, options Options) error {
	return s3cust.UpdateEndpoint(stack, s3cust.UpdateEndpointOptions{
		Accessor: s3cust.UpdateEndpointParameterAccessor{
			GetBucketFromInput: getPutObjectLockConfigurationBucketMember,
		},
		UsePathStyle:                   options.UsePathStyle,
		UseAccelerate:                  options.UseAccelerate,
		SupportsAccelerate:             true,
		TargetS3ObjectLambda:           false,
		EndpointResolver:               options.EndpointResolver,
		EndpointResolverOptions:        options.EndpointOptions,
		UseARNRegion:                   options.UseARNRegion,
		DisableMultiRegionAccessPoints: options.DisableMultiRegionAccessPoints,
	})
}
