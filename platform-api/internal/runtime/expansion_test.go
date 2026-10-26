package runtime

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strings"
	"testing"
)

func TestErrorBudgetAndCounterBoundaries(t *testing.T) {
	if math.Abs(burn(10000, 9990, 999000)-1) > 1e-9 {
		t.Fatal("incorrect burn")
	}
	if burn(0, 0, 999000) != 0 {
		t.Fatal("empty interval fabricated errors")
	}
	for _, c := range []RequestCounters{{Epoch: "e", Total: 1, Errors: 2}, {Epoch: "e", Total: 10, Le100: 4, Le300: 2}, {Total: 1}, {Epoch: "e", Total: -1}} {
		if c.valid() {
			t.Fatal("invalid counters accepted")
		}
	}
	c := RequestCounters{Epoch: "e", Total: 100, Errors: 1, Le100: 90, Le300: 95, Le1000: 99}
	if !c.valid() || c.good(SLO{Indicator: "availability"}) != 99 || c.good(SLO{Indicator: "latency", LatencyMS: 300}) != 95 {
		t.Fatal("incorrect instrumented counts")
	}
}
func TestVersionedTemplatesAreSelfContained(t *testing.T) {
	for _, id := range []string{"go-http", "node-http"} {
		data, e := templateArchive(id, "example-api")
		if e != nil {
			t.Fatal(e)
		}
		reader, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if e != nil {
			t.Fatal(e)
		}
		paths := map[string]bool{}
		for _, file := range reader.File {
			if !strings.HasPrefix(file.Name, "example-api/") || strings.Contains(file.Name, "..") {
				t.Fatal("unsafe archive")
			}
			r, e := file.Open()
			if e != nil {
				t.Fatal(e)
			}
			content, _ := io.ReadAll(r)
			r.Close()
			if bytes.Contains(content, []byte("__APP_NAME__")) {
				t.Fatal("unexpanded name")
			}
			paths[strings.TrimPrefix(file.Name, "example-api/")] = true
			if strings.HasSuffix(file.Name, "mini-fury-template.json") && !json.Valid(content) {
				t.Fatal("invalid provenance")
			}
		}
		for _, path := range []string{"Dockerfile", "README.md", "mini-fury-template.json", ".github/workflows/verify.yml", "kubernetes/deployment.yml"} {
			if !paths[path] {
				t.Fatal("missing template file", path)
			}
		}
	}
	if _, e := templateArchive("go-http", "../escape"); e == nil {
		t.Fatal("unsafe application name")
	}
}
func TestCapacityEstimatesAndUnits(t *testing.T) {
	p := CostPolicy{CPUCoreHour: 0.02, MemoryGiBHour: 0.01, StorageGiBMonth: 0.1}
	if math.Abs(estimateMonthly(p, 1, 2, 10)-30.2) > 1e-9 {
		t.Fatal("incorrect declared estimate")
	}
	for raw, want := range map[string]float64{"250m": 0.25, "100000000n": 0.1, "2": 2} {
		got, ok := cpuQuantity(raw)
		if !ok || math.Abs(got-want) > 1e-9 {
			t.Fatal("CPU quantity", raw)
		}
	}
	if n, ok := memoryQuantity("1024Mi"); !ok || n != 1 {
		t.Fatal("memory quantity")
	}
	if validPrice(math.NaN(), 100) || validPrice(-1, 100) {
		t.Fatal("invalid tariff accepted")
	}
}
