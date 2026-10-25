package main

// Every external host this program reaches out to, in one place. Mirrors
// src/config.js: update checks read package.json from Repo's Branch; scene
// packs are published to Repo's "scenes" GitHub release.

const (
	Repo   = "lazyjerry/whatsmyagentdoing"
	Branch = "main"

	// false: nothing reaches GitHub until the "Check for updates" button on the
	// page is pressed. true: the page checks on load and every 30 minutes, and
	// the CLI checks on start-up. WMAD_NO_UPDATE_CHECK=1 turns checks off either way.
	AutoUpdateCheck = false

	RepoURL   = "https://github.com/" + Repo
	LatestURL = "https://raw.githubusercontent.com/" + Repo + "/" + Branch + "/package.json"
)

func sceneReleaseURL(file string) string {
	return RepoURL + "/releases/download/scenes/" + file
}
