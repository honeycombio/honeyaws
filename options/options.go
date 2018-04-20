package options

type Options struct {
	Dataset    string `short:"d" long:"dataset" description:"Name of the dataset" default:"aws-$SERVICE-access"`
	SampleRate int    `long:"samplerate" description:"Only send 1 / N log lines" default:"1"`
	WriteKey   string `short:"k" long:"writekey" description:"Honeycomb team write key"`
	StateDir   string `long:"statedir" description:"Directory where ingest state is stored" default:"."`

	Version bool   `short:"V" long:"version" description:"Show version"`
	APIHost string `hidden:"true" long:"api_host" description:"Host for the Honeycomb API" default:"https://api.honeycomb.io/"`
	Debug   bool   `long:"debug" description:"Print debugging output"`
}
