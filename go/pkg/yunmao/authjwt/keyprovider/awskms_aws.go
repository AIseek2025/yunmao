//go:build kms_aws

// Package keyprovider awskms_aws.go：基于 aws-sdk-go-v2 的 AwsKmsSigner 真实实现。
//
// 用 `-tags kms_aws` 启用；启用前先添加依赖：
//
//	go get github.com/aws/aws-sdk-go-v2/config
//	go get github.com/aws/aws-sdk-go-v2/service/kms
//
// 与 ADR-0017 对齐：使用 RSA_2048 + RSASSA_PKCS1_V1_5_SHA_256，KeyId 即 kid。
package keyprovider

import (
	"context"
	"crypto/rsa"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
)

// AwsSdkSigner 基于 aws-sdk-go-v2 的 AwsKmsSigner 实现。
type AwsSdkSigner struct {
	cli   *kms.Client
	keyID string
}

// NewAwsSdkSigner 使用默认 SDK 配置初始化；keyID 是 KMS Key ID 或 ARN。
func NewAwsSdkSigner(ctx context.Context, keyID string) (*AwsSdkSigner, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	return &AwsSdkSigner{cli: kms.NewFromConfig(cfg), keyID: keyID}, nil
}

// SignDigest 走 KMS Sign，输入 SHA-256 摘要。
func (s *AwsSdkSigner) SignDigest(ctx context.Context, digest []byte) ([]byte, error) {
	out, err := s.cli.Sign(ctx, &kms.SignInput{
		KeyId:            aws.String(s.keyID),
		Message:          digest,
		MessageType:      types.MessageTypeDigest,
		SigningAlgorithm: types.SigningAlgorithmSpecRsassaPkcs1V15Sha256,
	})
	if err != nil {
		return nil, err
	}
	return out.Signature, nil
}

// PublicKey 走 KMS GetPublicKey，返回 *rsa.PublicKey + kid。
func (s *AwsSdkSigner) PublicKey(ctx context.Context) (*rsa.PublicKey, string, error) {
	out, err := s.cli.GetPublicKey(ctx, &kms.GetPublicKeyInput{
		KeyId: aws.String(s.keyID),
	})
	if err != nil {
		return nil, "", err
	}
	pub, err := ParsePKIXPublicKey(out.PublicKey)
	if err != nil {
		return nil, "", err
	}
	kid := s.keyID
	if out.KeyId != nil {
		kid = *out.KeyId
	}
	return pub, "awskms:" + kid, nil
}
