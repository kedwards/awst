package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// posixShellInit defines an awst() function that wraps the binary. Known
// subcommands pass straight through; a bare first arg (or `login`) is treated
// as a profile, run through `login --export`, and its stdout eval'd so the
// credential env vars land in the current shell — the assume <profile> UX.
const posixShellInit = `awst() {
  case "${1:-}" in
    creds|connect|console|exec|run|list|kill|logout|config|sso|shell|completion|help|--help|-h|--version|-v|"")
      command awst "$@" ;;
    login)
      shift; eval "$(command awst login --export "$@")" ;;
    *)
      eval "$(command awst login --export "$@")" ;;
  esac
}
`

// powershellShellInit is the PowerShell equivalent: passthrough for known
// subcommands, otherwise login --export piped through Invoke-Expression.
const powershellShellInit = `function awst {
  $passthrough = 'creds','connect','console','exec','run','list','kill','logout','config','sso','shell','completion','help','--help','-h','--version','-v'
  if ($args.Count -eq 0 -or $passthrough -contains $args[0]) {
    & (Get-Command awst -CommandType Application) @args
    return
  }
  $rest = if ($args[0] -eq 'login') { $args[1..($args.Count-1)] } else { $args }
  & (Get-Command awst -CommandType Application) login --export --shell powershell @rest | Invoke-Expression
}
`

func newShellCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "shell",
		Short: "Shell integration helpers",
	}
	c.AddCommand(newShellInitCmd())
	return c
}

func newShellInitCmd() *cobra.Command {
	var powershell bool
	c := &cobra.Command{
		Use:   "init",
		Short: "Print the awst shell function to eval in your rc file",
		Long: `Print a shell function named awst that wraps the binary so that
` + "`awst <profile>`" + ` logs in and sets the AWS credential env vars in the
current shell (the same UX as ` + "`assume <profile>`" + `). Known subcommands
pass through unchanged.

Add to your shell startup file:

  bash/zsh (~/.bashrc, ~/.zshrc):
    eval "$(awst shell init)"

  PowerShell ($PROFILE):
    awst shell init --powershell | Out-String | Invoke-Expression`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if powershell {
				fmt.Fprint(cmd.OutOrStdout(), powershellShellInit)
			} else {
				fmt.Fprint(cmd.OutOrStdout(), posixShellInit)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&powershell, "powershell", false, "Emit a PowerShell function instead of POSIX (bash/zsh)")
	return c
}
