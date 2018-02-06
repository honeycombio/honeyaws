package options

type Options struct {
	Dataset    string `long:"dataset" description:"Name of the dataset" default:"aws-$SERVICE-access"`
	SampleRate int    `long:"samplerate" description:"Only send 1 / N log lines" default:"1"`
	WriteKey   string `long:"writekey" description:"Honeycomb team write key"`
	StateDir   string `long:"statedir" description:"Directory where ingest state is stored" default:"."`
	HighAvail  bool   `long:"highavail" description:"Enable high availability ingestion using DynamoDB"`
	BackfillHr int    `long:"backfill" description:"The number of hours to increase backfill of log ingestion to with max of 168 hours (1 week)" default:"1"`

	Version bool   `short:"V" long:"version" description:"Show version"`
	APIHost string `hidden:"true" long:"api_host" description:"Host for the Honeycomb API" default:"https://api.honeycomb.io/"`
	Debug   bool   `long:"debug" description:"Print debugging output"`
}
