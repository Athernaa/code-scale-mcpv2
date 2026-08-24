package framework

import "strings"

type registry struct{ adapters []Adapter }

func defaultRegistry() registry {
	return registry{adapters: []Adapter{
		qbxAdapter{}, qbcoreAdapter{}, esxAdapter{}, oxInventoryAdapter{}, oxLibAdapter{}, oxTargetAdapter{},
	}}
}

func (r registry) provider(resource string, apis map[string]bool, evidence Evidence) (string, bool) {
	for _, adapter := range r.adapters {
		if framework, ok := adapter.ProviderFramework(resource, apis, evidence); ok {
			return framework, true
		}
	}
	return FrameworkCustom, false
}

func (r registry) operation(framework, api string, args []literalValue) (string, map[string]any, bool) {
	for _, adapter := range r.adapters {
		if operation, metadata, ok := adapter.CallOperation(framework, api, args); ok {
			return operation, metadata, true
		}
	}
	return "", nil, false
}

func knownResource(resource string, names ...string) bool {
	resource = strings.ToLower(resource)
	for _, name := range names {
		if resource == name {
			return true
		}
	}
	return false
}

type qbxAdapter struct{}

func (qbxAdapter) Name() string { return "qbx" }
func (qbxAdapter) ProviderFramework(resource string, apis map[string]bool, evidence Evidence) (string, bool) {
	if !knownResource(resource, "qbx_core") {
		return "", false
	}
	return "qbx", apis["GetPlayer"] || apis["GetCoreObject"] || apis["SetJob"]
}
func (qbxAdapter) CallOperation(framework, api string, args []literalValue) (string, map[string]any, bool) {
	if framework != "qbx" {
		return "", nil, false
	}
	switch api {
	case "GetPlayer", "GetCoreObject":
		return mapOperation("player_lookup", nil)
	case "SetJob":
		return mapOperation("player_job_set", jobMetadata(args))
	case "AddMoney":
		return mapOperation("player_money_add", moneyMetadata(args))
	case "RemoveMoney":
		return mapOperation("player_money_remove", moneyMetadata(args))
	case "SetMetaData":
		return mapOperation("player_metadata_set", nil)
	}
	return "", nil, false
}

type qbcoreAdapter struct{}

func (qbcoreAdapter) Name() string { return "qbcore" }
func (qbcoreAdapter) ProviderFramework(resource string, apis map[string]bool, evidence Evidence) (string, bool) {
	if !knownResource(resource, "qb-core") {
		return "", false
	}
	return "qbcore", apis["GetCoreObject"] || apis["GetPlayer"]
}
func (qbcoreAdapter) CallOperation(framework, api string, args []literalValue) (string, map[string]any, bool) {
	if framework != "qbcore" {
		return "", nil, false
	}
	switch api {
	case "GetCoreObject", "GetPlayer":
		return mapOperation("player_lookup", nil)
	case "AddMoney":
		return mapOperation("player_money_add", moneyMetadata(args))
	case "RemoveMoney":
		return mapOperation("player_money_remove", moneyMetadata(args))
	case "SetJob":
		return mapOperation("player_job_set", jobMetadata(args))
	case "SetMetaData":
		return mapOperation("player_metadata_set", nil)
	}
	return "", nil, false
}

type esxAdapter struct{}

func (esxAdapter) Name() string { return "esx" }
func (esxAdapter) ProviderFramework(resource string, apis map[string]bool, evidence Evidence) (string, bool) {
	if !knownResource(resource, "es_extended") {
		return "", false
	}
	return "esx", apis["getSharedObject"] || apis["GetPlayerFromId"]
}
func (esxAdapter) CallOperation(framework, api string, args []literalValue) (string, map[string]any, bool) {
	if framework != "esx" {
		return "", nil, false
	}
	switch api {
	case "getSharedObject", "GetPlayerFromId":
		return mapOperation("player_lookup", nil)
	case "addMoney", "addAccountMoney":
		return mapOperation("player_money_add", moneyMetadata(args))
	case "removeMoney", "removeAccountMoney":
		return mapOperation("player_money_remove", moneyMetadata(args))
	case "setJob":
		return mapOperation("player_job_set", jobMetadata(args))
	}
	return "", nil, false
}

type oxInventoryAdapter struct{}

func (oxInventoryAdapter) Name() string { return "ox_inventory" }
func (oxInventoryAdapter) ProviderFramework(resource string, apis map[string]bool, evidence Evidence) (string, bool) {
	if !knownResource(resource, "ox_inventory") {
		return "", false
	}
	known := 0
	for _, api := range []string{"AddItem", "RemoveItem", "Search", "GetInventory", "CanCarryItem", "SetMetadata", "RegisterStash"} {
		if apis[api] {
			known++
		}
	}
	return "ox_inventory", known >= 2
}
func (oxInventoryAdapter) CallOperation(framework, api string, args []literalValue) (string, map[string]any, bool) {
	if framework != "ox_inventory" {
		return "", nil, false
	}
	switch api {
	case "AddItem":
		return mapOperation("inventory_add_item", itemMetadata(args))
	case "RemoveItem":
		return mapOperation("inventory_remove_item", itemMetadata(args))
	case "Search", "GetInventory":
		return mapOperation("inventory_query", nil)
	case "CanCarryItem":
		return mapOperation("inventory_capacity_check", nil)
	case "SetMetadata":
		return mapOperation("inventory_metadata_set", nil)
	case "RegisterStash":
		return mapOperation("inventory_stash_register", nil)
	}
	return "", nil, false
}

type oxLibAdapter struct{}

func (oxLibAdapter) Name() string { return "ox_lib" }
func (oxLibAdapter) ProviderFramework(resource string, apis map[string]bool, evidence Evidence) (string, bool) {
	if !knownResource(resource, "ox_lib") {
		return "", false
	}
	for _, api := range []string{"notify", "progressBar", "progressCircle", "showContext", "inputDialog", "callback"} {
		if apis[api] {
			return "ox_lib", true
		}
	}
	return "", false
}
func (oxLibAdapter) CallOperation(framework, api string, args []literalValue) (string, map[string]any, bool) {
	if framework != "ox_lib" {
		return "", nil, false
	}
	switch api {
	case "callback", "callback.await", "callback.call":
		return mapOperation("callback_call", nil)
	case "callback.register":
		return mapOperation("callback_register", nil)
	case "notify":
		return mapOperation("notification", nil)
	case "progressBar", "progressCircle":
		return mapOperation("progress_ui", nil)
	case "showContext":
		return mapOperation("context_menu_register", nil)
	case "inputDialog":
		return mapOperation("input_dialog", nil)
	}
	return "", nil, false
}

type oxTargetAdapter struct{}

func (oxTargetAdapter) Name() string { return "ox_target" }
func (oxTargetAdapter) ProviderFramework(resource string, apis map[string]bool, evidence Evidence) (string, bool) {
	if !knownResource(resource, "ox_target") {
		return "", false
	}
	for api := range apis {
		if strings.HasPrefix(api, "add") || strings.HasPrefix(api, "Add") || api == "removeGlobalOption" {
			return "ox_target", true
		}
	}
	return "", false
}
func (oxTargetAdapter) CallOperation(framework, api string, args []literalValue) (string, map[string]any, bool) {
	if framework != "ox_target" {
		return "", nil, false
	}
	if strings.HasPrefix(api, "add") || strings.HasPrefix(api, "Add") {
		return mapOperation("interaction_register", nil)
	}
	return "", nil, false
}

func mapOperation(name string, metadata map[string]any) (string, map[string]any, bool) {
	return name, metadata, true
}
func literalString(args []literalValue, index int) string {
	if index >= len(args) || args[index].Kind != "string" {
		return ""
	}
	return args[index].Value
}
func literalNumber(args []literalValue, index int) string {
	if index >= len(args) || args[index].Kind != "number" {
		return ""
	}
	return args[index].Value
}
func moneyMetadata(args []literalValue) map[string]any {
	m := map[string]any{}
	if v := literalString(args, 0); v != "" {
		m["money_type"] = v
	}
	if v := literalNumber(args, 1); v != "" {
		m["amount_literal"] = v
	}
	return m
}
func jobMetadata(args []literalValue) map[string]any {
	m := map[string]any{}
	if v := literalString(args, 0); v != "" {
		m["job_literal"] = v
	}
	if v := literalNumber(args, 1); v != "" {
		m["grade_literal"] = v
	}
	return m
}
func itemMetadata(args []literalValue) map[string]any {
	m := map[string]any{}
	if v := literalString(args, 1); v != "" {
		m["item_literal"] = v
	}
	if v := literalNumber(args, 2); v != "" {
		m["count_literal"] = v
	}
	return m
}
