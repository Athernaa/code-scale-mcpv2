local Audit = {}
local Permissions = {
    moderator = { 'character:inspect', 'vehicle:inspect' },
    admin = { 'character:inspect', 'vehicle:inspect', 'inventory:grant', 'property:unlock' },
}

local function hasPermission(group, permission)
    for _, value in ipairs(Permissions[group] or {}) do
        if value == permission then
            return true
        end
    end
    return false
end

local function record(source, action, target)
    Audit[#Audit + 1] = { source = source, action = action, target = target, at = os.time() }
end

function IsAllowed(source, permission)
    local player = exports.core:GetPlayer(source)
    local group = player and player.metadata and player.metadata.group or 'user'
    return hasPermission(group, permission)
end

function InspectCharacter(source, target)
    if not IsAllowed(source, 'character:inspect') then
        return false, 'forbidden'
    end
    record(source, 'inspect_character', target)
    return true, exports.characters:GetCharacter(target)
end

function GrantItem(source, target, item, count)
    if not IsAllowed(source, 'inventory:grant') then
        return false, 'forbidden'
    end
    record(source, 'grant_item', target)
    return exports.inventory:AddItem(target, item, count)
end

RegisterCommand('admin:grant', function(source, _, args)
    GrantItem(source, tonumber(args[1]), args[2], tonumber(args[3]) or 1)
end)

RegisterCommand('admin:inspect', function(source, _, args)
    InspectCharacter(source, tonumber(args[1]))
end)

exports('InspectCharacter', InspectCharacter)
exports('GrantItem', GrantItem)
exports('IsAllowed', IsAllowed)
