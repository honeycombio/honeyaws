package options

// Options for the honeyelb command line
type Options struct {
	Config func(s string) error `long:"config" description:"INI config file" no-ini:"true"`

	APIHost    string `hidden:"true" long:"api_host" description:"Host for the Honeycomb API" default:"https://api.honeycomb.io/" env:"HONEYELB_API_HOST"`
	Dataset    string `long:"dataset" description:"Name of the dataset" default:"aws-elb-access" env:"HONEYELB_DATASET"`
	SampleRate int    `long:"samplerate" description:"Only send 1 / N log lines" default:"1" env:"HONEYELB_SAMPLE_RATE"`
	StateDir   string `long:"statedir" description:"Directory where ingest state is stored" default:"." env:"HONEYELB_STATE_DIR"`
	WriteKey   string `long:"writekey" description:"Honeycomb team write key" env:"HONEYELB_WRITE_KEY"`

	Debug   bool `long:"debug" description:"Print debugging output" env:"HONEYELB_DEBUG"`
	Version bool `short:"V" long:"version" description:"Show version"`
}
