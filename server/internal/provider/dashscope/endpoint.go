package dashscope

import (
	"errors"
	"fmt"
	"strings"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
)

// regionEndpoints 照抄 server.js:161-165 的 REGION_ENDPOINTS。
var regionEndpoints = map[string]string{
	"cn-beijing":     "https://dashscope.aliyuncs.com",
	"ap-southeast-1": "https://dashscope-intl.aliyuncs.com",
	"us-east-1":      "https://dashscope-us.aliyuncs.com",
}

// defaultRegion 是未知 region 的兜底目标，对应 server.js:191
// `REGION_ENDPOINTS[region] || REGION_ENDPOINTS['cn-beijing']`。
const defaultRegion = "cn-beijing"

// euCentralRegion 是唯一不在 regionEndpoints 表里、需要 workspaceId 拼模板
// 域名的 region（server.js:186-192 getEndpoint 的特殊分支）。
const euCentralRegion = "eu-central-1"

// resolveRegionURL 照抄 server.js getEndpoint(region, workspaceId)：
//
//   - eu-central-1：拼 `https://{workspaceId}.eu-central-1.maas.aliyuncs.com`；
//     缺 workspaceId 时旧系统直接 throw，未捕获会变成进程内 500。这里改成
//     返回 apperr.ErrValidation——这是设计文档里明确列出的"改进项"，不是
//     要保留的旧行为：调用方传错配置应该拿到一个可读的 422，而不是服务端
//     内部错误。
//   - 已知 region：查表返回。
//   - 未知 region（包括空字符串）：兜底到 cn-beijing。
func resolveRegionURL(region, workspaceID string) (string, error) {
	if region == euCentralRegion {
		if workspaceID == "" {
			return "", apperr.ErrValidation.Wrap(
				errors.New("dashscope: region=eu-central-1 需要 workspaceId"),
			)
		}
		return fmt.Sprintf("https://%s.eu-central-1.maas.aliyuncs.com", workspaceID), nil
	}
	if base, ok := regionEndpoints[region]; ok {
		return base, nil
	}
	return regionEndpoints[defaultRegion], nil
}

// baseURL 照抄 server.js resolveEndpoint(endpoint, region, workspaceId)：
// 显式 endpoint 以 "http" 开头时整段覆盖（去掉结尾的斜杠），否则走
// region 推导。
func (c *Client) baseURL() (string, error) {
	if strings.HasPrefix(c.endpoint, "http") {
		return strings.TrimRight(c.endpoint, "/"), nil
	}
	return resolveRegionURL(c.region, c.workspaceID)
}
