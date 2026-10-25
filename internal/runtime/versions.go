package runtime

import (
	"encoding/json"
	"errors"
	"regexp"

	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
	"github.com/shaoxier/AIofCage/internal/store"
)

const AgentDefaultsKey = "agent_default_versions"

var versionPattern = regexp.MustCompile(`^[0-9]{1,6}\.[0-9]{1,6}\.[0-9]{1,6}(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`)

func ValidAgentVersion(version string) bool {
	return len(version) <= 100 && versionPattern.MatchString(version)
}

// 旧沙箱没有版本字段；固定为其创建年代的版本，不跟随未来应用默认值漂移。
var legacyAgentVersions = map[session.Agent]string{session.Codex: "0.164.0", session.ClaudeCode: "2.1.309", session.DSH: "0.2.1-rc.2"}

func PinnedAgentVersions(s sandbox.Sandbox) map[session.Agent]string {
	versions := map[session.Agent]string{}
	for _, agent := range s.Agents() {
		versions[agent] = s.AgentVersions[agent]
		if versions[agent] == "" {
			versions[agent] = legacyAgentVersions[agent]
		}
	}
	return versions
}

func DefaultAgentVersions(st *store.Store) (map[session.Agent]string, error) {
	versions := map[session.Agent]string{}
	if raw := st.Setting(AgentDefaultsKey); raw != "" && json.Unmarshal([]byte(raw), &versions) != nil {
		return nil, errors.New("智能体默认版本配置损坏")
	}
	for agent, version := range versions {
		if !agent.Valid() || !ValidAgentVersion(version) {
			return nil, errors.New("智能体默认版本无效")
		}
	}
	for _, spec := range Catalog() {
		if versions[spec.Agent] == "" {
			versions[spec.Agent] = spec.Version
		}
	}
	return versions, nil
}
