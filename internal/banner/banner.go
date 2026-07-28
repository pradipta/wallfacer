// Package banner holds the wallfacer ASCII-art logo shared by the CLI help
// output and the interactive TUI splash.
package banner

// Art is the wallfacer ASCII-art logo. It has a leading and trailing newline
// so callers can drop it straight into help text or a splash screen.
const Art = `
██╗    ██╗█████╗  ██╗     ██╗     ███████╗█████╗   ██████╗███████╗██████╗
██║    ██║██╔══██╗██║     ██║     ██╔════╝██╔══██╗██╔════╝██╔════╝██╔══██╗
██║ █╗ ██║███████║██║     ██║     █████╗  ███████║██║     █████╗  ██████╔╝
██║███╗██║██╔══██║██║     ██║     ██╔══╝  ██╔══██║██║     ██╔══╝  ██╔══██╗
╚███╔███╔╝██║  ██║███████╗███████╗██║     ██║  ██║╚██████╗███████╗██║  ██║
 ╚══╝╚══╝ ╚═╝  ╚═╝╚══════╝╚══════╝╚═╝     ╚═╝  ╚═╝ ╚═════╝╚══════╝╚═╝  ╚═╝
`
