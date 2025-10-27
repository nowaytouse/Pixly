package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// completionCmd represents the completion command
var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "🔧 生成shell自动补全脚本",
	Long: `生成指定shell的自动补全脚本。

要启用自动补全，请运行以下命令之一：

Bash:
  source <(pixly completion bash)
  # 或者将其添加到 ~/.bashrc:
  echo 'source <(pixly completion bash)' >> ~/.bashrc

Zsh:
  source <(pixly completion zsh)
  # 或者将其添加到 ~/.zshrc:
  echo 'source <(pixly completion zsh)' >> ~/.zshrc

Fish:
  pixly completion fish | source
  # 或者将其添加到 ~/.config/fish/config.fish:
  echo 'pixly completion fish | source' >> ~/.config/fish/config.fish

PowerShell:
  pixly completion powershell | Out-String | Invoke-Expression
  # 或者将其添加到PowerShell配置文件`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	Run: func(cmd *cobra.Command, args []string) {
		switch args[0] {
		case "bash":
			if err := cmd.Root().GenBashCompletion(os.Stdout); err != nil {
				cmd.PrintErrf("Error generating bash completion: %v\n", err)
			}
		case "zsh":
			if err := cmd.Root().GenZshCompletion(os.Stdout); err != nil {
				cmd.PrintErrf("Error generating zsh completion: %v\n", err)
			}
		case "fish":
			if err := cmd.Root().GenFishCompletion(os.Stdout, true); err != nil {
				cmd.PrintErrf("Error generating fish completion: %v\n", err)
			}
		case "powershell":
			if err := cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout); err != nil {
				cmd.PrintErrf("Error generating powershell completion: %v\n", err)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}
