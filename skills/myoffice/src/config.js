'use strict';

// Every external host this project reaches out to, in one place. Update checks
// read package.json from REPO's BRANCH; scene packs are published to REPO's
// "scenes" GitHub release. Change REPO here when the project moves or is forked.
//
// scenes.json stores only each pack's file name (pinned by sha256); scenes.js
// builds the download URL from sceneReleaseUrl() at read time.

const REPO = 'lazyjerry/whatsmyagentdoing';
const BRANCH = 'main';

// Update checks. false: nothing reaches GitHub until the "Check for updates"
// button on the page is pressed. true: the page checks on load and every 30
// minutes, and the CLI checks on start-up and prints an "Update available"
// line. WMAD_NO_UPDATE_CHECK=1 turns checks off either way.
const AUTO_UPDATE_CHECK = false;

// Where this skill lives inside the repo (the clone root also holds go-app/ and docs/).
const SKILL_PATH = 'skills/myoffice';

const REPO_URL = `https://github.com/${REPO}`;
const LATEST_URL = `https://raw.githubusercontent.com/${REPO}/${BRANCH}/${SKILL_PATH}/package.json`;

function sceneReleaseUrl(file) {
  return `${REPO_URL}/releases/download/scenes/${file}`;
}

module.exports = { REPO, BRANCH, REPO_URL, LATEST_URL, AUTO_UPDATE_CHECK, sceneReleaseUrl };
