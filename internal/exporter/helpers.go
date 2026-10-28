package exporter

import (
	"encoding/json"

	"github.com/prometheus/client_golang/prometheus"
	"gitlab.com/torn-city/go-torn-client/client"
)

func gauge(name, help string, value float64, base prometheus.Labels, extraKV ...string) prometheus.Metric {
	labels := cloneLabels(base)
	for i := 0; i+1 < len(extraKV); i += 2 {
		labels[extraKV[i]] = extraKV[i+1]
	}
	desc := prometheus.NewDesc(prometheus.BuildFQName(namespace, "", name), help, nil, labels)
	return prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value)
}

func withLabel(base prometheus.Labels, key, value string) prometheus.Labels {
	out := cloneLabels(base)
	out[key] = value
	return out
}

func cloneLabels(in prometheus.Labels) prometheus.Labels {
	out := make(prometheus.Labels, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func boolFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

func optionalFloat(v *int32) float64 {
	if v == nil {
		return 0
	}
	return float64(*v)
}

func emptyAs(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func statusState(status client.UserStatus) string {
	if v, err := status.State.AsUserStatusStateEnum(); err == nil && v != "" {
		return string(v)
	}
	if v, err := status.State.AsUserStatusState1(); err == nil && v != "" {
		return v
	}
	return "unknown"
}

func applyJob(snap *accountSnapshot, resp *client.UserJobResponse) {
	if resp == nil || resp.Job == nil {
		return
	}
	jobUnion := *resp.Job

	raw, err := jobUnion.MarshalJSON()
	if err != nil || string(raw) == "null" {
		return
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return
	}

	jobType := stringField(generic, "type")
	switch jobType {
	case "company":
		if company, err := jobUnion.AsUserCompany(); err == nil && company.Name != "" {
			snap.jobType = "company"
			snap.jobName = company.Name
			snap.jobPosition = company.Position
			return
		}
	default:
		if job, err := jobUnion.AsUserJob(); err == nil && job.Name != "" {
			snap.jobType = string(job.Type)
			snap.jobName = string(job.Name)
			snap.jobPosition = jobPositionString(job.Position)
			return
		}
	}

	snap.jobType = jobType
	snap.jobName = stringField(generic, "name")
	snap.jobPosition = stringField(generic, "position")
}

func jobPositionString(position client.UserJob_Position) string {
	raw, err := position.MarshalJSON()
	if err != nil {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	return string(raw)
}

func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch typed := v.(type) {
	case string:
		return typed
	default:
		b, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		var s string
		if err := json.Unmarshal(b, &s); err == nil {
			return s
		}
		return string(b)
	}
}
