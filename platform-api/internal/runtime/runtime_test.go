package runtime

import (
	"strings"
	"testing"
)

func TestScopeAndConfigurationValidation(t *testing.T) {
	in := ScopeInput{Name: "staging-api", Environment: "staging", Criticality: "low", ClusterID: "lab", Namespace: "fixture", Replicas: 1, CPUMilli: 100, MemoryMiB: 128}
	if in.Validate() != nil {
		t.Fatal("valid scope rejected")
	}
	in.Replicas = 1000
	if in.Validate() == nil {
		t.Fatal("unbounded replicas accepted")
	}
	binding := Binding{Secrets: map[string]SecretReference{"database": {Name: "private-database", Key: "password"}}}
	for _, c := range []Configuration{{Variables: map[string]string{"PASSWORD": "must-not-persist"}}, {SecretRefs: map[string]string{"DB_PASSWORD": "unapproved"}}, {Variables: map[string]string{"MESSAGE": strings.Repeat("x", 5000)}}} {
		if validateConfiguration(c, binding) == nil {
			t.Fatal("unsafe configuration accepted")
		}
	}
	if validateConfiguration(Configuration{Variables: map[string]string{"MESSAGE": "ready"}, SecretRefs: map[string]string{"DB_PASSWORD": "database"}}, binding) != nil {
		t.Fatal("approved secret reference rejected")
	}
	repo := RepositoryConfig{ImagePrefixes: []string{"registry.example.com/team/"}}
	digest := strings.Repeat("a", 64)
	if !allowedImage(repo, "registry.example.com/team/api@sha256:"+digest) || allowedImage(repo, "registry.example.com/other/api@sha256:"+digest) || allowedImage(repo, "registry.example.com/team/api:latest") {
		t.Fatal("image boundary or digest validation failed")
	}
}
