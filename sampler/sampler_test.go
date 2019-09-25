package sampler

import (
	"testing"

	"github.com/honeycombio/dynsampler-go"

	"github.com/honeycombio/honeyaws/options"
)

func TestNewSamplerFromOptions(t *testing.T) {
	opt := &options.Options{}
	s, err := NewSamplerFromOptions(opt)
	if err != ErrUnknownSamplerType {
		t.Error("expected ErrUnknownSamplerType for empty options")
	}

	opt.SamplerType = SamplerTypeSimple
	opt.SamplerInterval = 5
	opt.SampleRate = 3

	s, err = NewSamplerFromOptions(opt)
	if err != nil {
		t.Errorf("unexpected error %s", err.Error())
	}
	avgSampler := s.(*dynsampler.AvgSampleRate)
	if avgSampler.ClearFrequencySec != opt.SamplerInterval || avgSampler.GoalSampleRate != opt.SampleRate {
		t.Error("got AvgSampleRate sampler without correct values")
	}

	opt.SamplerType = SamplerTypeEMA
	opt.SamplerDecay = 0.22

	s, err = NewSamplerFromOptions(opt)
	if err != nil {
		t.Errorf("unexpected error %s", err.Error())
	}
	emaSampler := s.(*dynsampler.EMASampleRate)
	if emaSampler.AdjustmentInterval != opt.SamplerInterval || emaSampler.GoalSampleRate != opt.SampleRate ||
		emaSampler.Weight != opt.SamplerDecay {
		t.Error("got EMASampleRate sampler without correct values")
	}
}
