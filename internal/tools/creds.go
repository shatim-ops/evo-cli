// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import "github.com/shatim-ops/evo-cli/internal/s3"

// accessKey returns the resolved access key (flags, env, profile) for the
// mk8s OIDC exchange, which needs the key itself rather than an IAM token.
func (s *Registry) accessKey() (keyID, secret string) {
	eng, err := s.newEngine(nil)
	if err != nil || eng == nil {
		return "", ""
	}
	return eng.Config.KeyID, eng.Config.Secret
}

// s3Config returns the resolved Object Storage settings: CLOUDRU_S3_* or
// the s3 section of the profile.
func (s *Registry) s3Config() (s3.Config, error) {
	eng, err := s.newEngine(nil)
	if err != nil {
		return s3.Config{}, err
	}
	if eng == nil {
		return s3.Config{}, nil
	}
	c := eng.Config.S3
	return s3.Config{Endpoint: c.Endpoint, AccessKey: c.AccessKey, SecretKey: c.SecretKey, Region: c.Region}, nil
}
