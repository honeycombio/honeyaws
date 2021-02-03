package publisher

import (
	"strings"

	"github.com/honeycombio/honeytail/event"
	"github.com/honeycombio/urlshaper"
	"github.com/sirupsen/logrus"
)

type requestShaper struct {
	pr *urlshaper.Parser
}

// Nicked directly from github.com/honeycombio/honeytail/leash.go
func (rs *requestShaper) Shape(field string, ev *event.Event) {
	if val, ok := ev.Data[field]; ok {
		// start by splitting out method, uri, and version
		parts := strings.Split(val.(string), " ")
		var path string
		if len(parts) == 3 {
			// treat it as METHOD /path HTTP/1.X
			ev.Data[field+"_method"] = parts[0]
			ev.Data[field+"_protocol_version"] = parts[2]
			path = parts[1]
		} else {
			// treat it as just the /path
			path = parts[0]
		}

		// next up, get all the goodies out of the path
		res, err := rs.pr.Parse(path)
		if err != nil {
			// couldn't parse it, just pass along the event
			logrus.WithError(err).Error("Couldn't parse request")
			return
		}
		ev.Data[field+"_uri"] = res.URI
		ev.Data[field+"_path"] = res.Path
		if res.Query != "" {
			ev.Data[field+"_query"] = res.Query
		}
		ev.Data[field+"_shape"] = res.Shape
		if res.QueryShape != "" {
			ev.Data[field+"_queryshape"] = res.QueryShape
		}
		for k, v := range res.PathFields {
			ev.Data[field+"_path_"+k] = v[0]
		}
	}
}
