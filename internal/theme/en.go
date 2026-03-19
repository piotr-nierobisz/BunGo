package theme

// English locale: CLI copy, dev UI copy, scaffold errors/templates keys.

type cliLocale struct {
	RootShort           string
	RootHelpBodyFmt     string
	RootVersionReactFmt string
	RootHelpParagraph1  string
	RootHelpParagraph2  string
	RootBannerTitle     string

	InitShort       string
	InitSuccessFmt  string
	InitModeFmt     string
	InitNextSteps   string
	InitCommandsFmt string
	FlagTypescript  string

	DevShort            string
	DevLong             string
	FlagEntry           string
	ErrGoModNotFoundFmt string
}

type devUILocale struct {
	ASCIIBanner           string
	Title                 string
	Description           string
	LabelBunGoVersion     string
	LabelReactRuntime     string
	LabelProjectRoot      string
	LabelRunTarget        string
	FooterShuttingDown    string
	FooterWatchingLineFmt string
	FooterWatchingText    string
	FooterPressCtrlC      string
	Initializing          string
}

type devLocale struct {
	AppExitWithErrFmt  string
	AppExitOK          string
	RestartFailedFmt   string
	ChangeReloadingFmt string
	UI                 devUILocale
}

type scaffoldLocale struct {
	ErrProjectNameRequired     string
	ErrProjectNameSingleDirFmt string
	ErrTargetExistsFmt         string
	ErrTargetAccessFmt         string
	ErrWritingFmt              string
	GoModBodyFmt               string
}

// localeBundle groups all English strings. Named type avoids "missing type in composite literal"
// when nested values use keyed literals (anonymous outer structs + nested literals can confuse older parsers).
type localeBundle struct {
	CLI      cliLocale
	Dev      devLocale
	Scaffold scaffoldLocale
}

// EN is the default English locale bundle. Add e.g. FR as another localeBundle value later.
var EN = localeBundle{
	CLI: cliLocale{
		RootShort:           "BunGo: The uncompromised Go + React framework",
		RootHelpBodyFmt:     "%s\n%s\n\n%s\n%s",
		RootVersionReactFmt: "Version: %s | React Runtime: %s",
		RootHelpParagraph1:  "BunGo empowers you to build full-stack Go web apps with embedded React in seconds.",
		RootHelpParagraph2:  "Use it to scaffold new projects and run the lightning-fast development server.",
		RootBannerTitle:     "🚀 BunGo CLI",

		InitShort:       "Scaffold a new BunGo project",
		InitSuccessFmt:  "🎉 Successfully created BunGo project at %s",
		InitModeFmt:     "⚙️  Frontend mode: %s",
		InitNextSteps:   "🚀 Ready to dive in? Run the following commands:",
		InitCommandsFmt: "  cd %s\n  go mod tidy\n  bungo dev",
		FlagTypescript:  "Scaffold TypeScript views and add tsconfig.json",

		DevShort:            "Start the BunGo development server",
		DevLong:             "Starts a hot-reloading development server that watches your Go and React files, automatically rebuilding and refreshing the browser on changes.",
		FlagEntry:           "Go entry target passed to `go run` (for example `.` or `./cmd/server`)",
		ErrGoModNotFoundFmt: "❌ Error: go.mod not found in %s. Please run `bungo dev` from the root of your project",
	},
	Dev: devLocale{
		AppExitWithErrFmt:  "App process exited: %v\n",
		AppExitOK:          "App process exited.\n",
		RestartFailedFmt:   "Restart failed: %v\n",
		ChangeReloadingFmt: "[%s] change detected, reloading…\n",
		UI: devUILocale{
			ASCIIBanner: `
██████╗ ██╗   ██╗███╗   ██╗██████╗  ██████╗ 
██╔══██╗██║   ██║████╗  ██║██╔════╝ ██╔═══██╗
██████╔╝██║   ██║██╔██╗ ██║██║  ███╗██║   ██║
██╔══██╗██║   ██║██║╚██╗██║██║   ██║██║   ██║
██████╔╝╚██████╔╝██║ ╚████║╚██████╔╝╚██████╔╝
╚═════╝  ╚═════╝ ╚═╝  ╚═══╝ ╚═════╝  ╚═════╝ 
`,
			Title: "BUNGO DEV SERVER",
			Description: "Welcome to the BunGo Development Environment. This server is continuously watching your Go,\n" +
				"template, and React (JSX/TSX) files. When changes are detected, it will safely restart the\n" +
				"backend process, rebuild frontend assets via embedded esbuild, and automatically refresh your\n" +
				"connected browser tabs. Experience uncompromised, lightning-fast full-stack Go development.",
			LabelBunGoVersion:     "📦 BunGo version: ",
			LabelReactRuntime:     "⚛️  React runtime: ",
			LabelProjectRoot:      "📂 Project root : ",
			LabelRunTarget:        "🎯 Run target   : ",
			FooterShuttingDown:    "Gracefully shutting down BunGo dev server... 👋",
			FooterWatchingLineFmt: "%s %s\n",
			FooterWatchingText:    "👀 Watching for file changes... (Auto-reloading enabled)",
			FooterPressCtrlC:      "🛑 Press Ctrl+C to stop the server.",
			Initializing:          "\n  Initializing BunGo Dev Server...",
		},
	},
	Scaffold: scaffoldLocale{
		ErrProjectNameRequired:     "project name is required",
		ErrProjectNameSingleDirFmt: "project name must be a single directory name, got %q",
		ErrTargetExistsFmt:         "target directory %q already exists",
		ErrTargetAccessFmt:         "unable to access target directory %q: %w",
		ErrWritingFmt:              "writing %s failed: %w",
		GoModBodyFmt:               "module %s\n\ngo %s\n",
	},
}
