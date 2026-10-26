package runtime

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
)

type SecretReference struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}
type Binding struct {
	Namespace     string                     `json:"namespace"`
	ApplicationID string                     `json:"applicationId"`
	Secrets       map[string]SecretReference `json:"secrets"`
}
type ClusterConfig struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Server    string    `json:"server"`
	CAFile    string    `json:"caFile"`
	TokenFile string    `json:"tokenFile"`
	Bindings  []Binding `json:"bindings"`
}
type RepositoryConfig struct {
	ApplicationID string   `json:"applicationId"`
	Provider      string   `json:"provider"`
	Repository    string   `json:"repository"`
	Ref           string   `json:"ref"`
	Workflow      string   `json:"workflow"`
	TokenFile     string   `json:"tokenFile"`
	AgentID       string   `json:"agentId"`
	ImagePrefixes []string `json:"imagePrefixes"`
	APIURL        string   `json:"apiUrl"`
}
type AgentConfig struct {
	ID        string `json:"id"`
	TokenFile string `json:"tokenFile"`
}
type ServiceProfile struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Engine       string `json:"engine"`
	Image        string `json:"image"`
	SecretAlias  string `json:"secretAlias"`
	CPUMilli     int    `json:"cpuMilli"`
	MemoryMiB    int    `json:"memoryMiB"`
	StorageClass string `json:"storageClass"`
}
type Config struct {
	ServiceProfiles []ServiceProfile   `json:"serviceProfiles"`
	Clusters        []ClusterConfig    `json:"clusters"`
	Repositories    []RepositoryConfig `json:"repositories"`
	Agents          []AgentConfig      `json:"agents"`
}

var clusterIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func LoadConfig(path string) (Config, error) {
	var c Config
	if path == "" {
		return c, nil
	}
	data, e := os.ReadFile(path)
	if e != nil {
		return c, errors.New("runtime connection configuration unavailable")
	}
	if len(data) > 128*1024 {
		return c, ErrInvalid
	}
	if e = json.Unmarshal(data, &c); e != nil {
		return c, errors.New("invalid runtime connection configuration")
	}
	seen := map[string]bool{}
	for _, v := range c.Clusters {
		if !clusterIDPattern.MatchString(v.ID) || seen[v.ID] || v.Server == "" || v.CAFile == "" || v.TokenFile == "" {
			return c, ErrInvalid
		}
		seen[v.ID] = true
		for _, b := range v.Bindings {
			if !uuidPattern.MatchString(b.ApplicationID) || !regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`).MatchString(b.Namespace) {
				return c, ErrInvalid
			}
		}
	}
	for _, p := range c.ServiceProfiles {
		if !clusterIDPattern.MatchString(p.ID) || (p.Engine != "postgres" && p.Engine != "redis") || !imagePattern.MatchString(p.Image) || p.CPUMilli < 100 || p.CPUMilli > 2000 || p.MemoryMiB < 128 || p.MemoryMiB > 2048 || p.SecretAlias == "" {
			return c, ErrInvalid
		}
	}
	return c, nil
}
func (c Config) binding(app, cluster, namespace string) (ClusterConfig, Binding, bool) {
	for _, v := range c.Clusters {
		if v.ID == cluster {
			for _, b := range v.Bindings {
				if b.ApplicationID == app && b.Namespace == namespace {
					return v, b, true
				}
			}
		}
	}
	return ClusterConfig{}, Binding{}, false
}
func (c Config) repository(app string) (RepositoryConfig, bool) {
	for _, r := range c.Repositories {
		if r.ApplicationID == app {
			return r, true
		}
	}
	return RepositoryConfig{}, false
}
