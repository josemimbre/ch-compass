package cli

import "github.com/spf13/cobra"

// connectionFlags holds the ClickHouse connection flags shared verbatim by
// every subcommand (analyze, census): host/port/user/password/secure/debug.
// Kept as one struct instead of duplicating the six StringVar/BoolVar calls
// (and their help text) in each subcommand's flag registration.
type connectionFlags struct {
	Host, Username, Password string
	Port                     int
	Secure, Debug            bool
}

// register adds the shared connection flags to cmd's flag set.
func (f *connectionFlags) register(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&f.Host, "host", "localhost", "ClickHouse host")
	flags.IntVarP(&f.Port, "port", "p", 8123, "ClickHouse HTTP port")
	flags.StringVarP(&f.Username, "user", "u", "", "ClickHouse username")
	flags.StringVar(&f.Password, "password", "", "ClickHouse password")
	flags.BoolVar(&f.Debug, "debug", false, "Print SQL queries as they are executed")
	flags.BoolVar(&f.Secure, "secure", false, "Use HTTPS for ClickHouse connection (TLS)")
}
