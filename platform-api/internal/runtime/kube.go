package runtime

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

type Kube struct {
	base      string
	client    *http.Client
	tokenFile string
}
type KubeError struct{ Status int }

func (e KubeError) Error() string { return fmt.Sprintf("kubernetes status %d", e.Status) }
func NewKube(c ClusterConfig) (*Kube, error) {
	u, e := url.Parse(c.Server)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return nil, ErrInvalid
	}
	ca, e := os.ReadFile(c.CAFile)
	if e != nil {
		return nil, errors.New("cluster CA unavailable")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, ErrInvalid
	}
	return &Kube{base: c.Server, tokenFile: c.TokenFile, client: &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, MaxConnsPerHost: 8, MaxIdleConnsPerHost: 4, MaxIdleConns: 16, IdleConnTimeout: 60 * time.Second, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (k *Kube) request(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	var data []byte
	var e error
	if body != nil {
		data, e = json.Marshal(body)
		if e != nil {
			return e
		}
	}
	raw := k.base + path
	if query != nil {
		raw += "?" + query.Encode()
	}
	req, e := http.NewRequestWithContext(ctx, method, raw, bytes.NewReader(data))
	if e != nil {
		return e
	}
	token, e := os.ReadFile(k.tokenFile)
	if e != nil {
		return errors.New("cluster identity unavailable")
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Content-Type", "application/json")
	res, e := k.client.Do(req)
	if e != nil {
		return errors.New("cluster connection failed")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return KubeError{res.StatusCode}
	}
	if out == nil {
		return nil
	}
	rawData, e := io.ReadAll(io.LimitReader(res.Body, 2*1024*1024+1))
	if e != nil || len(rawData) > 2*1024*1024 {
		return errors.New("cluster response too large")
	}
	return json.Unmarshal(rawData, out)
}

type ObjectMeta struct {
	Name            string            `json:"name"`
	Generation      int64             `json:"generation"`
	UID             string            `json:"uid"`
	ResourceVersion string            `json:"resourceVersion"`
	Labels          map[string]string `json:"labels"`
	Annotations     map[string]string `json:"annotations"`
	OwnerReferences []struct {
		Kind string `json:"kind"`
		UID  string `json:"uid"`
		Name string `json:"name"`
	} `json:"ownerReferences"`
}
type Pod struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Containers []struct {
			Name  string `json:"name"`
			Image string `json:"image"`
		} `json:"containers"`
	} `json:"spec"`
	Status struct {
		Phase             string `json:"phase"`
		ContainerStatuses []struct {
			Name         string          `json:"name"`
			Ready        bool            `json:"ready"`
			RestartCount int             `json:"restartCount"`
			State        json.RawMessage `json:"state"`
		} `json:"containerStatuses"`
	} `json:"status"`
}
type Deployment struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   ObjectMeta      `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
	Status     struct {
		ObservedGeneration int64 `json:"observedGeneration"`
		Replicas           int   `json:"replicas"`
		UpdatedReplicas    int   `json:"updatedReplicas"`
		ReadyReplicas      int   `json:"readyReplicas"`
		AvailableReplicas  int   `json:"availableReplicas"`
		Conditions         []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"conditions"`
	} `json:"status"`
}

func owned(meta ObjectMeta, s Scope) bool {
	return meta.Labels["mini-fury.io/application"] == s.ApplicationID && meta.Labels["mini-fury.io/scope"] == s.ID
}
func deploymentPath(s Scope) string {
	return "/apis/apps/v1/namespaces/" + s.Namespace + "/deployments/" + s.Workload
}
func (k *Kube) deployment(ctx context.Context, s Scope) (Deployment, error) {
	var d Deployment
	e := k.request(ctx, "GET", deploymentPath(s), nil, nil, &d)
	if e == nil && !owned(d.Metadata, s) {
		return d, ErrDenied
	}
	return d, e
}
func (k *Kube) pods(ctx context.Context, s Scope) ([]Pod, bool, error) {
	var data struct {
		Items    []Pod `json:"items"`
		Metadata struct {
			Continue string `json:"continue"`
		} `json:"metadata"`
	}
	e := k.request(ctx, "GET", "/api/v1/namespaces/"+s.Namespace+"/pods", url.Values{"labelSelector": {"mini-fury.io/application=" + s.ApplicationID + ",mini-fury.io/scope=" + s.ID + ",!mini-fury.io/service"}, "limit": {"100"}}, nil, &data)
	if e != nil {
		return nil, false, e
	}
	out := []Pod{}
	for _, p := range data.Items {
		if owned(p.Metadata, s) && p.Metadata.Labels["mini-fury.io/service"] == "" {
			out = append(out, p)
		}
	}
	return out, data.Metadata.Continue != "", nil
}
func (k *Kube) podOwned(ctx context.Context, s Scope, name string) (Pod, error) {
	var p Pod
	if !regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`).MatchString(name) {
		return p, ErrInvalid
	}
	e := k.request(ctx, "GET", "/api/v1/namespaces/"+s.Namespace+"/pods/"+name, nil, nil, &p)
	if e != nil {
		return p, e
	}
	if !owned(p.Metadata, s) {
		return p, ErrDenied
	}
	d, e := k.deployment(ctx, s)
	if e != nil {
		return p, e
	}
	for _, owner := range p.Metadata.OwnerReferences {
		if owner.Kind == "ReplicaSet" {
			var rs struct {
				Metadata ObjectMeta `json:"metadata"`
			}
			e = k.request(ctx, "GET", "/apis/apps/v1/namespaces/"+s.Namespace+"/replicasets/"+owner.Name, nil, nil, &rs)
			if e != nil {
				return p, e
			}
			if rs.Metadata.UID != owner.UID || !owned(rs.Metadata, s) {
				continue
			}
			for _, r := range rs.Metadata.OwnerReferences {
				if r.Kind == "Deployment" && r.UID == d.Metadata.UID {
					return p, nil
				}
			}
		}
	}
	return p, ErrDenied
}
func (k *Kube) logs(ctx context.Context, s Scope, pod, container string, lines int, previous bool) (string, bool, error) {
	p, e := k.podOwned(ctx, s, pod)
	if e != nil {
		return "", false, e
	}
	found := false
	for _, c := range p.Spec.Containers {
		if c.Name == container {
			found = true
		}
	}
	if !found || lines < 1 || lines > 500 {
		return "", false, ErrInvalid
	}
	q := url.Values{"container": {container}, "tailLines": {fmt.Sprint(lines)}, "limitBytes": {"131072"}, "timestamps": {"true"}, "previous": {fmt.Sprint(previous)}}
	req, e := http.NewRequestWithContext(ctx, "GET", k.base+"/api/v1/namespaces/"+s.Namespace+"/pods/"+pod+"/log?"+q.Encode(), nil)
	if e != nil {
		return "", false, e
	}
	token, e := os.ReadFile(k.tokenFile)
	if e != nil {
		return "", false, e
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	res, e := k.client.Do(req)
	if e != nil {
		return "", false, errors.New("pod logs unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", false, KubeError{res.StatusCode}
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, 131073))
	truncated := len(data) > 131072
	if truncated {
		data = data[:131072]
	}
	return string(data), truncated, e
}
