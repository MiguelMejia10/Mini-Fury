package runtime

import (
	"database/sql"
	"errors"
	"math"
	"mini-operador-ti/platform-api/internal/identity"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type CostPolicy struct {
	Currency        string    `json:"currency"`
	CPUCoreHour     float64   `json:"cpuCoreHour"`
	MemoryGiBHour   float64   `json:"memoryGiBHour"`
	StorageGiBMonth float64   `json:"storageGiBMonth"`
	MonthlyBudget   float64   `json:"monthlyBudget"`
	Version         int64     `json:"version"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func validPrice(v float64, max float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= max
}
func (h *HTTP) costPolicy(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if !validID(w, app) {
		return
	}
	var in CostPolicy
	if !decode(w, r, &in) {
		return
	}
	if in.Currency != "USD" || !validPrice(in.CPUCoreHour, 100) || !validPrice(in.MemoryGiBHour, 100) || !validPrice(in.StorageGiBMonth, 100) || in.MonthlyBudget < 1 || !validPrice(in.MonthlyBudget, 1000000) {
		problem(w, ErrInvalid)
		return
	}
	// Budget and prices are governance settings: a team/platform administrator is required.
	tx, e := h.appTx(r.Context(), app, true)
	if e != nil {
		problem(w, e)
		return
	}
	defer tx.Rollback()
	u, _ := identity.CurrentUser(r.Context())
	var version int64
	if in.Version == 0 {
		e = tx.QueryRowContext(r.Context(), `INSERT INTO application_cost_policies(application_id,currency,cpu_core_hour,memory_gib_hour,storage_gib_month,monthly_budget,updated_by)VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(application_id) DO NOTHING RETURNING version`, app, in.Currency, in.CPUCoreHour, in.MemoryGiBHour, in.StorageGiBMonth, in.MonthlyBudget, u.ID).Scan(&version)
	} else {
		e = tx.QueryRowContext(r.Context(), `UPDATE application_cost_policies SET cpu_core_hour=$2,memory_gib_hour=$3,storage_gib_month=$4,monthly_budget=$5,version=version+1,updated_by=$6,updated_at=now() WHERE application_id=$1 AND version=$7 RETURNING version`, app, in.CPUCoreHour, in.MemoryGiBHour, in.StorageGiBMonth, in.MonthlyBudget, u.ID, in.Version).Scan(&version)
	}

	if e != nil {
		problem(w, ErrConflict)
		return
	}
	if e = event(r.Context(), tx, "cost.policy_update", app); e == nil {
		e = tx.Commit()
	}
	if e != nil {
		problem(w, e)
		return
	}
	output(w, 200, map[string]any{"version": version})
}
func estimateMonthly(p CostPolicy, cpuCores, memoryGiB, storageGiB float64) float64 {
	return 730*(cpuCores*p.CPUCoreHour+memoryGiB*p.MemoryGiBHour) + storageGiB*p.StorageGiBMonth
}
func cpuQuantity(s string) (float64, bool) {
	factor := 1.0
	switch {
	case strings.HasSuffix(s, "n"):
		factor = 1e-9
		s = strings.TrimSuffix(s, "n")
	case strings.HasSuffix(s, "u"):
		factor = 1e-6
		s = strings.TrimSuffix(s, "u")
	case strings.HasSuffix(s, "m"):
		factor = 0.001
		s = strings.TrimSuffix(s, "m")
	}
	v, e := strconv.ParseFloat(s, 64)
	return v * factor, e == nil && v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}
func memoryQuantity(s string) (float64, bool) {
	factor := 1.0
	for _, unit := range []struct {
		suffix string
		factor float64
	}{{"Gi", 1073741824}, {"Mi", 1048576}, {"Ki", 1024}, {"G", 1e9}, {"M", 1e6}, {"K", 1e3}} {
		if strings.HasSuffix(s, unit.suffix) {
			factor = unit.factor
			s = strings.TrimSuffix(s, unit.suffix)
			break
		}
	}
	v, e := strconv.ParseFloat(s, 64)
	return v * factor / 1073741824, e == nil && v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}
func (h *HTTP) capacity(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if !validID(w, app) {
		return
	}
	if _, e := appAccess(r.Context(), h.DB, app); e != nil {
		problem(w, e)
		return
	}
	var p CostPolicy
	pe := h.DB.QueryRowContext(r.Context(), `SELECT currency,cpu_core_hour,memory_gib_hour,storage_gib_month,monthly_budget,version,updated_at FROM application_cost_policies WHERE application_id=$1`, app).Scan(&p.Currency, &p.CPUCoreHour, &p.MemoryGiBHour, &p.StorageGiBMonth, &p.MonthlyBudget, &p.Version, &p.UpdatedAt)
	if pe != nil && !errors.Is(pe, sql.ErrNoRows) {
		problem(w, pe)
		return
	}
	tx, e := h.DB.BeginTx(r.Context(), nil)
	canManage := false
	if e == nil {
		canManage = authorize(r.Context(), tx, app, true) == nil
		tx.Rollback()
	}
	rows, e := h.DB.QueryContext(r.Context(), `SELECT `+scopeColumns+` FROM runtime_scopes s WHERE application_id=$1 ORDER BY created_at DESC LIMIT 50`, app)
	if e != nil {
		problem(w, e)
		return
	}
	scopes := []Scope{}
	for rows.Next() {
		s, e := scanScope(rows)
		if e != nil {
			rows.Close()
			problem(w, e)
			return
		}
		scopes = append(scopes, s)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		problem(w, e)
		return
	}
	items := []map[string]any{}
	var cpu, mem, storage float64
	for _, s := range scopes {
		c := float64(s.Replicas*(s.CPUMilli/2)) / 1000
		m := float64(s.Replicas*(s.MemoryMiB/2)) / 1024
		cpu += c
		mem += m
		item := map[string]any{"scopeId": s.ID, "name": s.Name, "environment": s.Environment, "replicas": s.Replicas, "requestedCPUCores": c, "requestedMemoryGiB": m, "limitCPUCores": float64(s.Replicas*s.CPUMilli) / 1000, "limitMemoryGiB": float64(s.Replicas*s.MemoryMiB) / 1024}
		if pe == nil {
			item["estimatedMonthly"] = estimateMonthly(p, c, m, 0)
		}
		items = append(items, item)
	}
	if e = h.DB.QueryRowContext(r.Context(), `SELECT COALESCE(sum(replicas*(cpu_milli/2)/1000.0),0),COALESCE(sum(replicas*(memory_mib/2)/1024.0),0) FROM runtime_scopes WHERE application_id=$1`, app).Scan(&cpu, &mem); e != nil {
		problem(w, e)
		return
	}
	rows, e = h.DB.QueryContext(r.Context(), `SELECT v.state,v.storage_gib,v.profile FROM managed_services v JOIN runtime_scopes s ON s.id=v.scope_id WHERE s.application_id=$1`, app)
	if e != nil {
		problem(w, e)
		return
	}
	for rows.Next() {
		var state, profile string
		var gib int
		if e = rows.Scan(&state, &gib, &profile); e != nil {
			rows.Close()
			problem(w, e)
			return
		}
		storage += float64(gib)
		if state != "suspended" {
			if profile, ok := h.profile(profile); ok {
				cpu += float64(profile.CPUMilli/2) / 1000
				mem += float64(profile.MemoryMiB/2) / 1024
			}
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		problem(w, e)
		return
	}
	out := map[string]any{"scopes": items, "hasMoreScopes": len(scopes) == 50, "canManagePolicy": canManage, "estimateBasis": "Recursos declarados y tarifas ingresadas por el administrador, 730 horas/mes; no es facturación", "requestedCPUCores": cpu, "requestedMemoryGiB": mem, "retainedStorageGiB": storage, "measuredUsagePath": "/api/scopes/{scope}/kubernetes", "invoicedCost": nil, "policy": nil, "estimatedMonthly": nil, "overBudget": false}
	if pe == nil {
		estimate := estimateMonthly(p, cpu, mem, storage)
		out["policy"] = p
		out["estimatedMonthly"] = estimate
		out["overBudget"] = estimate > p.MonthlyBudget
		out["budgetUsedPercent"] = 100 * estimate / p.MonthlyBudget
	}
	output(w, 200, out)
}
