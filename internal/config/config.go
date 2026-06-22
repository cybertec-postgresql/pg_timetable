package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	flags "github.com/jessevdk/go-flags"
	"github.com/spf13/viper"
)

type cmdArg struct {
	fullName string
	*flags.Option
}

func (a cmdArg) HasChanged() bool {
	return a.IsSet() && !a.IsSetDefault()
}

func (a cmdArg) Name() string {
	return a.fullName
}

func (a cmdArg) ValueString() string {
	// For slice-valued flags (e.g. --file), fmt's default formatting would
	// produce a bracket-wrapped string like "[a b]", which viper then stores
	// as a single string. Join with the separator viper uses to split string
	// slices ("," ) so the value round-trips back into a []string correctly.
	if s, ok := a.Value().([]string); ok {
		return strings.Join(s, ",")
	}
	return fmt.Sprintf("%v", a.Value())
}

func (a cmdArg) ValueType() string {
	// Report the proper viper-recognized type for string slices so that
	// ValueString() is parsed back into a []string instead of a plain string.
	if _, ok := a.Value().([]string); ok {
		return "stringSlice"
	}
	return a.Field().Type.Name()
}

type cmdArgSet struct {
	*flags.Parser
}

func eachGroup(g *flags.Group, f func(*flags.Group)) {
	f(g)
	for _, gg := range g.Groups() {
		eachGroup(gg, f)
	}
}

func eachOption(g *flags.Group, f func(*flags.Group, *flags.Option)) {
	eachGroup(g, func(g *flags.Group) {
		for _, option := range g.Options() {
			f(g, option)
		}
	})
}

// VisitAll will execute fn() for all options found in command line.
// Since we have only two level of nesting it's enough to use simplified group-prefixed name.
func (cmdSet cmdArgSet) VisitAll(fn func(viper.FlagValue)) {
	root := cmdSet.Group.Find("Application Options")
	eachOption(root, func(g *flags.Group, o *flags.Option) {
		name := o.LongName
		if g != root {
			name = g.ShortDescription + cmdSet.NamespaceDelimiter + name
		}
		fn(cmdArg{name, o})
	})
}

func (cmdSet cmdArgSet) setDefaults(v *viper.Viper) {
	eachOption(cmdSet.Group, func(g *flags.Group, o *flags.Option) {
		if o.Default != nil && o.IsSetDefault() {
			name := o.LongName
			if g != cmdSet.Group {
				name = g.ShortDescription + cmdSet.NamespaceDelimiter + name
			}
			v.SetDefault(name, o.Value())
		}
	})
}

// NewConfig returns a new instance of CmdOptions
func NewConfig(writer io.Writer) (*CmdOptions, error) {
	v := viper.New()
	p, err := Parse(writer)
	if err != nil {
		return nil, err
	}
	flagSet := cmdArgSet{p}
	if err = v.BindFlagValues(flagSet); err != nil {
		return nil, fmt.Errorf("cannot bind command-line flag values with viper: %w", err)
	}
	flagSet.setDefaults(v)
	if v.IsSet("config") {
		v.SetConfigFile(v.GetString("config"))
		err := v.ReadInConfig() // Find and read the config file
		if err != nil {         // Handle errors reading the config file
			return nil, fmt.Errorf("fatal error reading config file: %w", err)
		}
	}
	conf := &CmdOptions{}
	if err = v.Unmarshal(conf); err != nil {
		return nil, fmt.Errorf("fatal error unmarshalling config file: %w", err)
	}
	// viper may bind the default value of the []string `file` flag as a single
	// empty string ([""]) when no --file is provided. Strip empty entries so
	// startup file processing is not triggered for non-existent paths.
	conf.Start.File = filterEmpty(conf.Start.File)
	if conf.ClientName == "" {
		buf := bytes.NewBufferString("The required flag `-c, --clientname` was not specified\n")
		p.WriteHelp(buf)
		return conf, errors.New(buf.String())
	}
	if err := ValidateOTel(conf.OTel); err != nil {
		return conf, err
	}
	return conf, nil
}

// filterEmpty returns a new slice with blank (empty or whitespace-only)
// strings removed and surrounding whitespace trimmed from the rest.
func filterEmpty(in []string) []string {
	out := in[:0]
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ValidateOTel validates OTelOpts fields and returns an error for invalid values.
func ValidateOTel(opts OTelOpts) error {
	if opts.SampleRatio < 0.0 || opts.SampleRatio > 1.0 {
		return errors.New("otel-sample-ratio must be between 0.0 and 1.0")
	}
	if opts.MetricPeriod <= 0 {
		return errors.New("otel-metric-period must be > 0")
	}
	if opts.ShutdownTimeout <= 0 {
		return errors.New("otel-shutdown-timeout must be > 0")
	}
	if opts.Endpoint != "" {
		u, err := url.Parse(opts.Endpoint)
		if err != nil {
			return fmt.Errorf("otel: invalid endpoint URL: %w", err)
		}
		switch u.Scheme {
		case "grpc", "http", "https":
			// valid
		default:
			return fmt.Errorf("unsupported OTel endpoint scheme: %s", u.Scheme)
		}
	}
	return nil
}
