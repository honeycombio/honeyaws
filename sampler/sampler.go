package sampler

import (
	"fmt"

	dynsampler "github.com/honeycombio/dynsampler-go"
	"github.com/honeycombio/honeyaws/options"
)

var ErrUnknownSamplerType = fmt.Errorf("unknown sampler type specified, supported types are: simple, ema")

const (
	SamplerTypeSimple = "simple"
	SamplerTypeEMA    = "ema"
)

func NewSamplerFromOptions(opt *options.Options) (dynsampler.Sampler, error) {
	switch opt.SamplerType {
	case SamplerTypeSimple:
		return &dynsampler.AvgSampleRate{
			ClearFrequencySec: opt.SamplerInterval,
			GoalSampleRate:    opt.SampleRate,
		}, nil
	case SamplerTypeEMA:
		return &dynsampler.EMASampleRate{
			AdjustmentInterval: opt.SamplerInterval,
			Weight:             opt.SamplerDecay,
			GoalSampleRate:     opt.SampleRate,
		}, nil
	default:
		return nil, ErrUnknownSamplerType
	}
}
