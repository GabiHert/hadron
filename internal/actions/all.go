package actions

// RegisterAll adds every action of this build to r. Later milestones add their files here.
func RegisterAll(r *Registry) {
	registerApp(r)
	registerUpdate(r)
	registerWorkspaces(r)
	registerColumns(r)
	registerCards(r)
	registerSessions(r)
	registerLaunchProfiles(r)
	registerUI(r)
	registerMCP(r)
	registerChat(r)
	registerQuestions(r)
	registerFields(r)
	registerRules(r)
	registerFilters(r)
	registerPrompts(r)
	registerIntegrations(r)
	registerLinks(r)
	registerDash(r)
	registerHistory(r)
	registerWorktrees(r)
	registerAdopt(r)
	registerPlanImport(r)
}
