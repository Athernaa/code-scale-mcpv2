local Properties = {}
local Visits = {}

local function ensureProperty(propertyId)
    if not Properties[propertyId] then
        Properties[propertyId] = { id = propertyId, owner = nil, interior = HousingConfig.defaultInterior, locked = true, visitors = {} }
    end
    return Properties[propertyId]
end

function RegisterProperty(propertyId, interior)
    if not propertyId or not HousingConfig.validInterior(interior) then
        return false, 'invalid_property'
    end
    local property = ensureProperty(propertyId)
    property.interior = interior
    return true, property
end

function SetPropertyOwner(source, propertyId)
    local property = ensureProperty(propertyId)
    local player = exports.core:GetPlayer(source)
    if not player then
        return false, 'player_missing'
    end
    property.owner = player.identifier
    property.locked = false
    return true
end

function EnterProperty(source, propertyId)
    local property = Properties[propertyId]
    if not property or property.locked then
        return false, 'property_locked'
    end
    property.visitors[source] = os.time()
    Visits[source] = propertyId
    TriggerClientEvent('atlas:propertyEntered', source, property)
    return true
end

function LeaveProperty(source)
    local propertyId = Visits[source]
    if not propertyId or not Properties[propertyId] then
        return false, 'not_inside'
    end
    Properties[propertyId].visitors[source] = nil
    Visits[source] = nil
    TriggerClientEvent('atlas:propertyLeft', source, propertyId)
    return true
end

function TogglePropertyLock(source, propertyId)
    local property = Properties[propertyId]
    local player = exports.core:GetPlayer(source)
    if not property or not player or property.owner ~= player.identifier then
        return false, 'not_owner'
    end
    property.locked = not property.locked
    return true, property.locked
end

RegisterNetEvent('atlas:propertyEnter')
AddEventHandler('atlas:propertyEnter', function(propertyId)
    EnterProperty(source, propertyId)
end)

RegisterNetEvent('atlas:propertyLeave')
AddEventHandler('atlas:propertyLeave', function()
    LeaveProperty(source)
end)

RegisterCommand('home:enter', function(source, _, args)
    EnterProperty(source, args[1])
end)

exports('RegisterProperty', RegisterProperty)
exports('SetPropertyOwner', SetPropertyOwner)
exports('EnterProperty', EnterProperty)
exports('LeaveProperty', LeaveProperty)
exports('TogglePropertyLock', TogglePropertyLock)
