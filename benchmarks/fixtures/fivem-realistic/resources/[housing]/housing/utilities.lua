local Utilities = {}
local UtilityState = {}

local function propertyUtility(propertyId, name)
    UtilityState[propertyId] = UtilityState[propertyId] or {}
    UtilityState[propertyId][name] = UtilityState[propertyId][name] or { enabled = true, usage = 0, limit = 100 }
    return UtilityState[propertyId][name]
end

function SetUtility(propertyId, name, enabled)
    local utility = propertyUtility(propertyId, name)
    utility.enabled = enabled == true
    return utility.enabled
end

function UseUtility(propertyId, name, amount)
    local utility = propertyUtility(propertyId, name)
    if not utility.enabled or utility.usage + amount > utility.limit then
        return false, 'utility_unavailable'
    end
    utility.usage = utility.usage + amount
    return true, utility
end

function ResetUtilities(propertyId)
    UtilityState[propertyId] = {}
    return true
end

function GetUtility(propertyId, name)
    return propertyUtility(propertyId, name)
end

function ListUtilities(propertyId)
    local result = {}
    for name, value in pairs(UtilityState[propertyId] or {}) do
        result[name] = value
    end
    return result
end

RegisterNetEvent('atlas:utilityUse')
AddEventHandler('atlas:utilityUse', function(propertyId, name, amount)
    UseUtility(propertyId, name, amount or 1)
end)

RegisterNetEvent('atlas:utilityToggle')
AddEventHandler('atlas:utilityToggle', function(propertyId, name, enabled)
    local player = exports.core:GetPlayer(source)
    if player and HasKey(propertyId, player.identifier, 'owner') then
        SetUtility(propertyId, name, enabled)
    end
end)

exports('SetUtility', SetUtility)
exports('UseUtility', UseUtility)
exports('GetUtility', GetUtility)
exports('ListUtilities', ListUtilities)
