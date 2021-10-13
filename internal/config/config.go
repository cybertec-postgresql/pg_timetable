package config

import (
	"errors"
	"fmt"
	"io"

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
	return fmt.Sprintf("%v", a.Value())
}

func (a cmdArg) ValueType() string {
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
	root := cmdSet.Parser.Group.Find("Application Options")
	eachOption(root, func(g *flags.Group, o *flags.Option) {
		name := o.LongName
		if g != root {
			name = g.ShortDescription + cmdSet.Parser.NamespaceDelimiter + name
		}
		fn(cmdArg{name, o})
	})
}

func (cmdSet cmdArgSet) setDefaults(v *viper.Viper) {
	eachOption(cmdSet.Parser.Group, func(g *flags.Group, o *flags.Option) {
		if o.Default != nil && o.IsSetDefault() {
			name := o.LongName
			if g != cmdSet.Parser.Group {
				name = g.ShortDescription + cmdSet.Parser.NamespaceDelimiter + name
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
			return nil, fmt.Errorf("Fatal error reading config file: %w", err)
		}
	}
	conf := &CmdOptions{}
	if err = v.Unmarshal(conf); err != nil {
		return nil, fmt.Errorf("Fatal error unmarshalling config file: %w", err)
	}
	if conf.ClientName == "" {
		p.WriteHelp(writer)
		return nil, errors.New("The required flag `-c, --clientname` was not specified")
	}
	return conf, nil
}
