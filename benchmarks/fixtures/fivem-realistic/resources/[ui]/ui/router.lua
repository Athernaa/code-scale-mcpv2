local Routes = {}
local CurrentRoute = nil
local History = {}

local function normalize(route)
    if not route or route == '' then
        return '/'
    end
    if string.sub(route, 1, 1) ~= '/' then
        return '/' .. route
    end
    return route
end

function RegisterRoute(route, handler)
    route = normalize(route)
    if type(handler) ~= 'function' then
        return false, 'handler_missing'
    end
    Routes[route] = handler
    return true
end

function Navigate(route, params)
    route = normalize(route)
    local handler = Routes[route]
    if not handler then
        return false, 'route_missing'
    end
    if CurrentRoute then
        History[#History + 1] = CurrentRoute
    end
    CurrentRoute = route
    local ok, value = pcall(handler, params or {})
    if not ok then
        return false, value
    end
    SendNUIMessage({ action = 'route', route = route, value = value })
    return true, value
end

function Back()
    local route = table.remove(History)
    if not route then
        return false, 'history_empty'
    end
    CurrentRoute = route
    SendNUIMessage({ action = 'route', route = route })
    return true, route
end

function Current()
    return CurrentRoute
end

function ListRoutes()
    local result = {}
    for route in pairs(Routes) do
        result[#result + 1] = route
    end
    table.sort(result)
    return result
end

RegisterRoute('/', function()
    return { panels = { 'inventory', 'jobs', 'housing' } }
end)

RegisterRoute('/inventory', function()
    TriggerEvent('atlas:inventoryRequest')
    return { title = 'Inventory' }
end)

RegisterRoute('/jobs', function()
    return { title = 'Jobs', duty = IsWorking() }
end)

RegisterRoute('/housing', function()
    return { title = 'Housing', property = GetCurrentProperty() }
end)

RegisterNUICallback('router:navigate', function(data, callback)
    local ok, value = Navigate(data.route, data.params)
    callback({ ok = ok, value = value })
end)

RegisterNUICallback('router:back', function(_, callback)
    local ok, value = Back()
    callback({ ok = ok, route = value })
end)

RegisterCommand('ui:route', function(_, args)
    Navigate(args[1])
end)

exports('RegisterRoute', RegisterRoute)
exports('Navigate', Navigate)
exports('CurrentRoute', Current)
exports('ListRoutes', ListRoutes)
