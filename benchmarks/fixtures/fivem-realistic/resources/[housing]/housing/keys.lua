local Keys = {}

local function propertyKeys(propertyId)
    if not Keys[propertyId] then
        Keys[propertyId] = {}
    end
    return Keys[propertyId]
end

function GrantKey(propertyId, identifier, role)
    local keys = propertyKeys(propertyId)
    keys[identifier] = { role = role or 'guest', grantedAt = os.time() }
    return true
end

function RevokeKey(propertyId, identifier)
    local keys = propertyKeys(propertyId)
    if not keys[identifier] then
        return false, 'key_missing'
    end
    keys[identifier] = nil
    return true
end

function HasKey(propertyId, identifier, requiredRole)
    local key = propertyKeys(propertyId)[identifier]
    if not key then
        return false
    end
    if requiredRole and key.role ~= requiredRole and key.role ~= 'owner' then
        return false
    end
    return true
end

function ListKeys(propertyId)
    return SharedUtils.copy(propertyKeys(propertyId))
end

function CanEnterWithKey(source, propertyId)
    local player = exports.core:GetPlayer(source)
    return player ~= nil and HasKey(propertyId, player.identifier)
end

RegisterNetEvent('atlas:propertyGrantKey')
AddEventHandler('atlas:propertyGrantKey', function(propertyId, identifier)
    local player = exports.core:GetPlayer(source)
    if player and HasKey(propertyId, player.identifier, 'owner') then
        GrantKey(propertyId, identifier, 'guest')
    end
end)

RegisterNetEvent('atlas:propertyRevokeKey')
AddEventHandler('atlas:propertyRevokeKey', function(propertyId, identifier)
    local player = exports.core:GetPlayer(source)
    if player and HasKey(propertyId, player.identifier, 'owner') then
        RevokeKey(propertyId, identifier)
    end
end)

exports('GrantKey', GrantKey)
exports('RevokeKey', RevokeKey)
exports('HasKey', HasKey)
exports('ListKeys', ListKeys)
