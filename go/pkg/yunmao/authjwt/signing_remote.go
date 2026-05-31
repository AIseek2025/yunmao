package authjwt

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

// RemoteSigner 抽象一个远端 RSA-PKCS1v15-SHA256 签名后端（Vault Transit / AWS KMS）。
//
// keyprovider.VaultSigningRef / keyprovider.AwsSigningRef 实现该接口，
// 通过 KeyProvider.Active().Material 暴露给本包 signClaims，
// 由 signingMethodRemoteRS256 完成实际签名。
type RemoteSigner interface {
	SignSHA256Digest(ctx context.Context, digest []byte) ([]byte, error)
}

// signingMethodRemoteRS256 自定义 jwt.SigningMethod；Alg 上报 RS256，
// Sign() 把 signingString 摘要后调用 RemoteSigner.SignSHA256Digest，
// Verify() 不实现（校验侧由 Verifier.Parse 标准 RS256 路径完成）。
type signingMethodRemoteRS256 struct{}

func (s *signingMethodRemoteRS256) Alg() string { return jwt.SigningMethodRS256.Alg() }

func (s *signingMethodRemoteRS256) Sign(signingString string, key any) ([]byte, error) {
	rs, ok := key.(RemoteSigner)
	if !ok {
		return nil, errors.New("authjwt: remote signing key not RemoteSigner")
	}
	h := sha256.Sum256([]byte(signingString))
	return rs.SignSHA256Digest(context.Background(), h[:])
}

func (s *signingMethodRemoteRS256) Verify(signingString string, sig []byte, key any) error {
	return errors.New("authjwt: remote signing method does not verify; use RS256")
}

var remoteRS256 = &signingMethodRemoteRS256{}

// RemoteRS256SigningMethod 暴露给外部（如 device-svc 自签 MQTT 凭证时复用）。
func RemoteRS256SigningMethod() jwt.SigningMethod { return remoteRS256 }
