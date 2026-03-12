package core

import "testing"

// BenchmarkApplyConditions measures performance of the condition processor
// (ApplyConditions) across several condition-set sizes.
func BenchmarkApplyConditions(b *testing.B) {
	msg := Message{
		ChannelName: "metrics",
		ServiceType: "bench",
		ServiceName: "benchsvc",
		Event:       "tick",
		Metric:      42.0,
		MetricName:  "cpu",
		State:       "running",
		Data: map[string]any{
			"level": "info",
			"count": 100,
		},
	}

	condsSmall := []ConditionConfig{
		{Field: "metric", Operator: ">", Value: 10},
		{Field: "metric", Operator: "<", Value: 100},
		{Field: "data.level", Operator: "eq", Value: "info"},
	}

	conds10 := make([]ConditionConfig, 0, 10)
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			conds10 = append(conds10, ConditionConfig{Field: "metric", Operator: ">", Value: 10 + i})
		} else {
			conds10 = append(conds10, ConditionConfig{Field: "data.count", Operator: ">", Value: i})
		}
	}

	conds100 := make([]ConditionConfig, 0, 100)
	for i := 0; i < 100; i++ {
		switch i % 3 {
		case 0:
			conds100 = append(conds100, ConditionConfig{Field: "metric", Operator: ">", Value: float64(i)})
		case 1:
			conds100 = append(conds100, ConditionConfig{Field: "data.level", Operator: "eq", Value: "info"})
		default:
			conds100 = append(conds100, ConditionConfig{Field: "data.count", Operator: ">=", Value: i})
		}
	}

	b.Run("small-3", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = ApplyConditions(msg, condsSmall)
		}
	})

	b.Run("medium-10", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = ApplyConditions(msg, conds10)
		}
	})

	b.Run("large-100", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = ApplyConditions(msg, conds100)
		}
	})
}
