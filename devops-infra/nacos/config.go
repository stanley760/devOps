package nacos

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

func FetchAll(dataIDs []string) (string, error) {
	for _, dataID := range dataIDs {
		if strings.TrimSpace(dataID) == "" {
			continue
		}
		return Fetch(dataID)
	}
	return "", nil
}

func Fetch(dataID string) (string, error) {
	if !isEnabled() {
		return "", nil
	}
	if strings.TrimSpace(dataID) == "" {
		return "", errors.New("nacos dataID cannot be empty")
	}
	client := getConfigClient()
	if client == nil {
		return "", errors.New("nacos config client is not initialized")
	}
	group := currentGroup()
	content, err := client.GetConfig(vo.ConfigParam{DataId: dataID, Group: group})
	if err != nil {
		return "", fmt.Errorf("get nacos config %q: %w", dataID, err)
	}
	return content, nil
}

func Listen(dataID string, onChange func(content string)) error {
	if !isEnabled() {
		return nil
	}
	if strings.TrimSpace(dataID) == "" {
		return errors.New("nacos dataID cannot be empty")
	}
	if onChange == nil {
		return errors.New("nacos config callback cannot be nil")
	}
	client := getConfigClient()
	if client == nil {
		return errors.New("nacos config client is not initialized")
	}
	group := currentGroup()
	return client.ListenConfig(vo.ConfigParam{
		DataId: dataID,
		Group:  group,
		OnChange: func(_, _, _, content string) {
			onChange(content)
		},
	})
}

func currentGroup() string {
	state.mu.RLock()
	defer state.mu.RUnlock()
	if strings.TrimSpace(state.cfg.ConfigCenter.Group) != "" {
		return state.cfg.ConfigCenter.Group
	}
	if strings.TrimSpace(state.cfg.Group) == "" {
		return constant.DEFAULT_GROUP
	}
	return state.cfg.Group
}
