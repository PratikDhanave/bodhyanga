package medical

import (
	"fmt"
	"strings"
)

// RPMObservation is a remote patient monitoring reading (RPM), inspired by
// connected devices used in chronic care programs (BP cuff, scale, pulse ox, etc.).
type RPMObservation struct {
	Device  string  `json:"device"`
	Metric  string  `json:"metric"`
	Value   float64 `json:"value"`
	Unit    string  `json:"unit"`
	TakenAt string  `json:"taken_at,omitempty"`
	Alert   bool    `json:"alert,omitempty"`
}

// EvaluateRPMAlerts returns human-readable alerts for out-of-range chronic-care vitals.
// Thresholds are illustrative demo values, not clinical guidelines.
func EvaluateRPMAlerts(obs []RPMObservation) []string {
	var alerts []string
	for _, o := range obs {
		if o.Alert {
			alerts = append(alerts, formatRPMAlert(o))
			continue
		}
		switch strings.ToLower(o.Metric) {
		case "systolic_bp":
			if o.Value >= 160 {
				alerts = append(alerts, formatRPMAlert(o))
			}
		case "weight_lb":
			if o.Value >= 185 {
				alerts = append(alerts, formatRPMAlert(o))
			}
		case "spo2":
			if o.Value < 92 {
				alerts = append(alerts, formatRPMAlert(o))
			}
		case "heart_rate":
			if o.Value >= 110 {
				alerts = append(alerts, formatRPMAlert(o))
			}
		}
	}
	return alerts
}

func formatRPMAlert(o RPMObservation) string {
	return fmt.Sprintf("%s=%.1f%s (%s)", o.Metric, o.Value, o.Unit, o.Device)
}
