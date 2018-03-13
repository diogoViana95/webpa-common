package zk

import (
	"github.com/Comcast/webpa-common/logging"
	"github.com/Comcast/webpa-common/service"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/go-kit/kit/sd"
	gokitzk "github.com/go-kit/kit/sd/zk"
)

func newService(r Registration) (string, gokitzk.Service) {
	url := service.FormatInstance(
		r.scheme(),
		r.address(),
		r.port(),
	)

	return url, gokitzk.Service{
		Path: r.path(),
		Name: r.name(),
		Data: []byte(url),
	}
}

// clientFactory is the factory function used to create a go-kit zookeeper Client.
// Tests can change this for mocked behavior.
var clientFactory = gokitzk.NewClient

func newClient(l log.Logger, zo Options) (gokitzk.Client, error) {
	return clientFactory(
		zo.servers(),
		l,
		gokitzk.ConnectTimeout(zo.connectTimeout()),
		gokitzk.SessionTimeout(zo.sessionTimeout()),
	)
}

func newInstancer(l log.Logger, c gokitzk.Client, path string) (i sd.Instancer, err error) {
	i, err = gokitzk.NewInstancer(c, path, l)
	if err == nil {
		i = service.NewContextualInstancer(i, map[string]interface{}{"path": path})
	}

	return
}

func newInstancers(l log.Logger, c gokitzk.Client, zo Options) (i service.Instancers, err error) {
	for _, path := range zo.watches() {
		if i.Has(path) {
			l.Log(level.Key(), level.WarnValue(), logging.MessageKey(), "skipping duplicate watch", "path", path)
			continue
		}

		var instancer sd.Instancer
		instancer, err = newInstancer(l, c, path)
		if err != nil {
			return
		}

		i.Set(path, instancer)
	}

	return
}

func newRegistrars(base log.Logger, c gokitzk.Client, zo Options) (r service.Registrars) {
	dedupe := make(map[string]bool)

	for _, registration := range zo.registrations() {
		endpoint, s := newService(registration)
		if dedupe[endpoint] {
			base.Log(level.Key(), level.WarnValue(), logging.MessageKey(), "skipping duplicate registration", "endpoint", endpoint)
			continue
		}

		dedupe[endpoint] = true
		r.Add(gokitzk.NewRegistrar(c, s, log.With(base, "endpoint", endpoint)))
	}

	return
}

// NewEnvironment constructs a Zookeeper-based service.Environment using both a zookeeper Options (typically unmarshaled
// from configuration) and an optional extra set of environment options.
func NewEnvironment(l log.Logger, zo Options, eo ...service.Option) (service.Environment, error) {
	if l == nil {
		l = logging.DefaultLogger()
	}

	if len(zo.Watches) == 0 && len(zo.Registrations) == 0 {
		return nil, nil
	}

	c, err := newClient(l, zo)
	if err != nil {
		return nil, err
	}

	i, err := newInstancers(l, c, zo)
	if err != nil {
		c.Stop()
		return nil, err
	}

	r := newRegistrars(l, c, zo)

	eo = append(
		eo,
		service.WithRegistrar(r),
		service.WithInstancers(i),
		service.WithCloser(func() error { c.Stop(); return nil }),
	)

	return service.NewEnvironment(eo...), nil
}
