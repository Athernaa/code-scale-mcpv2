package semantic

// ExportSidesCompatible reports whether a FiveM export call can execute
// against a provider on the declared sides. Unknown sides never establish a
// complete local provider proof. Shared callers require shared providers;
// client/server callers may use their own side or shared providers.
func ExportSidesCompatible(caller, provider string) bool {
	caller = NormalizeSide(caller)
	provider = NormalizeSide(provider)
	switch caller {
	case "client":
		return provider == "client" || provider == "shared"
	case "server":
		return provider == "server" || provider == "shared"
	case "shared":
		return provider == "shared"
	default:
		return false
	}
}
