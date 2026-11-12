package options

import (
	"reflect"
	"testing"

	flags "github.com/jessevdk/go-flags"
)

func TestSamplerFieldsFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{name: "defaults"},
		{name: "configured", args: []string{"--sampler_fields=domain_name", "--sampler_fields=elb_status_code"}, want: []string{"domain_name", "elb_status_code"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var opt Options
			if _, err := flags.NewParser(&opt, flags.None).ParseArgs(tc.args); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(opt.SamplerFields, tc.want) {
				t.Fatalf("fields = %#v, want %#v", opt.SamplerFields, tc.want)
			}
		})
	}
}
