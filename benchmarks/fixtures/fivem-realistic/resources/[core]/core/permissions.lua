local PermissionGroups = {
    user = { 'profile:read', 'inventory:read' },
    moderator = { 'profile:read', 'inventory:read', 'character:inspect' },
    admin = { 'profile:read', 'inventory:read', 'character:inspect', 'inventory:grant', 'property:unlock' },
}

local function groupPermissions(group)
    return PermissionGroups[group] or PermissionGroups.user
end

function HasPermission(source, permission)
    local player = GetPlayer(source)
    local group = player and player.metadata and player.metadata.group or 'user'
    for _, value in ipairs(groupPermissions(group)) do
        if value == permission then
            return true
        end
    end
    return false
end

function SetPermissionGroup(source, group)
    if not PermissionGroups[group] then
        return false, 'group_missing'
    end
    local player = GetPlayer(source)
    if not player then
        return false, 'player_missing'
    end
    player.metadata.group = group
    TriggerClientEvent('atlas:permissionChanged', source, group, groupPermissions(group))
    return true
end

function ListPermissions(source)
    local player = GetPlayer(source)
    local group = player and player.metadata and player.metadata.group or 'user'
    return group, groupPermissions(group)
end

function CanManageTarget(source, target)
    if source == target then
        return true
    end
    return HasPermission(source, 'character:inspect')
end

function RequirePermission(source, permission)
    if not HasPermission(source, permission) then
        TriggerClientEvent('atlas:notify', source, 'Permission denied', 'error')
        return false
    end
    return true
end

RegisterNetEvent('atlas:setGroup')
AddEventHandler('atlas:setGroup', function(target, group)
    if RequirePermission(source, 'property:unlock') and CanManageTarget(source, target) then
        SetPermissionGroup(target, group)
    end
end)

RegisterCommand('permission:list', function(source)
    local group, permissions = ListPermissions(source)
    TriggerClientEvent('atlas:permissionList', source, group, permissions)
end)

exports('HasPermission', HasPermission)
exports('SetPermissionGroup', SetPermissionGroup)
exports('ListPermissions', ListPermissions)
exports('RequirePermission', RequirePermission)
