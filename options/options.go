package options

type Options struct {
	Debug      bool   `long:"debug" description:"Print debugging output"`
	Dataset    string `long:"dataset" description:"Name of the dataset" default:"aws-elb-access"`
	SampleRate int    `long:"samplerate" description:"Only send 1 / N log lines" default:"2"`
	WriteKey   string `long:"writekey" description:"Honeycomb team write key"`
	StateDir   string `long:"statedir" description:"Directory where ingest state is stored" default:"."`
}
