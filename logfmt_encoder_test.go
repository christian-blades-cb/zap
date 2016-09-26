package zap

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"math"
	"testing"
)

func TestReplacementCharValue(t *testing.T) {
	if '\ufffd' <= ' ' {
		t.Error("value out of ident range")
	}
}

func withLogfmtEncoder(f func(*logfmtEncoder)) {
	enc := newLogfmtEncoder()
	f(enc)
	enc.Free()
}

func assertLogfmtOutput(t *testing.T, desc, expected string, f func(Encoder)) {
	withLogfmtEncoder(func(enc *logfmtEncoder) {
		f(enc)
		assert.Equal(t, expected, string(enc.bytes), "Unexpected encoder output after adding a %s.", desc)
	})
	withLogfmtEncoder(func(enc *logfmtEncoder) {
		enc.AddString("foo", "bar")
		f(enc)
		expectedPrefix := `foo="bar"`
		if expected != "" {
			expectedPrefix += " "
		}

		assert.Equal(t, expectedPrefix+expected, string(enc.bytes), "Unexpected encoder output after adding a %s as a second field.", desc)
	})
}

func TestLogfmtEncoderFields(t *testing.T) {
	tests := []struct {
		desc     string
		expected string
		f        func(Encoder)
	}{
		{"string", `k=v`, func(e Encoder) { e.AddString("k", "v") }},
		{"string", `k="v"`, func(e Encoder) { e.AddString("k", "") }},
		{"bool", "k", func(e Encoder) { e.AddBool("k", true) }},
		{"bool", "k=false", func(e Encoder) { e.AddBool("k", false) }},
		{"int", "k=42", func(e Encoder) { e.AddInt("k", 42) }},
		{"int64", "k=42", func(e Encoder) { e.AddInt64("k", 42) }},
		{"int64", fmt.Sprintf("k=%d", math.MaxInt64), func(e Encoder) { e.AddInt64("k", math.MaxInt64) }},
		{"uint", "k=42", func(e Encoder) { e.AddUint("k", 42) }},
		{"uint64", "k=42", func(e Encoder) { e.AddUint64("k", 42) }},
		{"uint64", fmt.Sprintf("k=%d", uint64(math.MaxUint64)), func(e Encoder) { e.AddUint64("k", math.MaxUint64) }},
		{"float64", "k=1", func(e Encoder) { e.AddFloat64("k", 1.0) }},
		{"float64", "k=10000000000", func(e Encoder) { e.AddFloat64("k", 1e10) }},
		{"float64", "k=NaN", func(e Encoder) { e.AddFloat64("k", math.NaN()) }},
		{"float64", "k=+Inf", func(e Encoder) { e.AddFloat64("k", math.Inf(1)) }},
		{"float64", "k=-Inf", func(e Encoder) { e.AddFloat64("k", math.Inf(-1)) }},
		{"marshaler", `loggable="yes"`, func(e Encoder) {
			assert.NoError(t, e.AddMarshaler("k", loggable{true}), "Unexpected error calling MarshalLog.")
		}},
		{"marshaler", "k={}", func(e Encoder) {
			assert.Error(t, e.AddMarshaler("k", loggable{false}), "Expected an error calling MarshalLog.")
		}},
		{"map[string]string", `k="map[loggable:yes]"`, func(e Encoder) {
			assert.NoError(t, e.AddObject("k", map[string]string{"loggable": "yes"}), "Unexpected error serializing a map.")
		}},
		{"arbitrary object", `k="{Name:jane}"`, func(e Encoder) {
			assert.NoError(t, e.AddObject("k", struct{ Name string }{"jane"}), "Unexpected error serializing a struct.")
		}},
	}

	for _, tt := range tests {
		assertLogfmtOutput(t, tt.desc, tt.expected, tt.f)
	}
}
