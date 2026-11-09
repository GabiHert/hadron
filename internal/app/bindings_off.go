//go:build !bindings

package app

// bindingsMode is true in the binary Wails builds to generate the frontend bindings. That binary
// only reflects on App's methods; it must never open the real database.
const bindingsMode = false
