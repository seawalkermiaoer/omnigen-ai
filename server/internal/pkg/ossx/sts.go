package ossx

import (
	"context"
	"fmt"
	"time"

	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	stsclient "github.com/alibabacloud-go/sts-20150401/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
)

// stsEndpoint mirrors server.js:114 (`endpoint: 'sts.aliyuncs.com'`) — the
// global STS endpoint, not a regional one. AssumeRole works the same
// regardless of which OSS region the bucket lives in.
const stsEndpoint = "sts.aliyuncs.com"

// stsAssumeRoleClient is the production AssumeRoleClient, wrapping the
// generated alibabacloud STS SDK (module sts-20150401/v2 — the same API
// version the old server.js called via @alicloud/sts20150401, see
// server.js:9,113-117).
type stsAssumeRoleClient struct {
	client *stsclient.Client
}

// NewSTSAssumeRoleClient constructs a client bound to a fixed RAM user
// AccessKeyID/Secret — the pair configured as oss_access_key_id /
// oss_access_key_secret in app_settings, allowed to assume oss_role_arn.
func NewSTSAssumeRoleClient(accessKeyID, accessKeySecret string) (AssumeRoleClient, error) {
	cfg := &openapiutil.Config{
		AccessKeyId:     tea.String(accessKeyID),
		AccessKeySecret: tea.String(accessKeySecret),
		Endpoint:        tea.String(stsEndpoint),
	}
	c, err := stsclient.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("构建 STS 客户端失败: %w", err)
	}
	return &stsAssumeRoleClient{client: c}, nil
}

// AssumeRole implements AssumeRoleClient.
func (s *stsAssumeRoleClient) AssumeRole(ctx context.Context, roleArn, sessionName string, durationSeconds int64) (Credentials, error) {
	req := &stsclient.AssumeRoleRequest{
		RoleArn:         tea.String(roleArn),
		RoleSessionName: tea.String(sessionName),
		DurationSeconds: tea.Int64(durationSeconds),
	}
	resp, err := s.client.AssumeRoleWithContext(ctx, req, &dara.RuntimeOptions{})
	if err != nil {
		return Credentials{}, fmt.Errorf("STS AssumeRole 失败: %w", err)
	}
	if resp == nil || resp.Body == nil || resp.Body.Credentials == nil {
		return Credentials{}, fmt.Errorf("STS AssumeRole 返回了空的 Credentials")
	}

	body := resp.Body.Credentials
	expiration := tea.StringValue(body.Expiration)
	exp, err := time.Parse(time.RFC3339, expiration)
	if err != nil {
		return Credentials{}, fmt.Errorf("STS AssumeRole 返回的 Expiration 无法解析 (%q): %w", expiration, err)
	}

	return Credentials{
		AccessKeyID:     tea.StringValue(body.AccessKeyId),
		AccessKeySecret: tea.StringValue(body.AccessKeySecret),
		SecurityToken:   tea.StringValue(body.SecurityToken),
		Expiration:      exp,
	}, nil
}

var _ AssumeRoleClient = (*stsAssumeRoleClient)(nil)
