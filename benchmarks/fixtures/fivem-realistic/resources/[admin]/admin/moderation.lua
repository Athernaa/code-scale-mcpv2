local Actions = {}
local Active = {}
local ActionTypes = { warn = true, mute = true, kick = true, ban = true }

local function actionId()
    return SharedUtils.newId('moderation', #Actions + 1)
end

function CreateAction(source, target, actionType, reason, duration)
    if not ActionTypes[actionType] or not reason or reason == '' then
        return false, 'action_invalid'
    end
    if not IsAllowed(source, 'character:inspect') then
        return false, 'forbidden'
    end
    local action = { id = actionId(), source = source, target = target, type = actionType, reason = reason, duration = duration, at = os.time(), active = true }
    Actions[#Actions + 1] = action
    Active[target] = action
    record(source, 'moderation_' .. actionType, target)
    TriggerClientEvent('atlas:moderationAction', target, action)
    return true, action
end

function GetActiveAction(target)
    return Active[target]
end

function ClearAction(source, target)
    if not IsAllowed(source, 'character:inspect') then
        return false, 'forbidden'
    end
    local action = Active[target]
    if not action then
        return false, 'action_missing'
    end
    action.active = false
    Active[target] = nil
    TriggerClientEvent('atlas:moderationCleared', target, action.id)
    return true
end

function ListActions(target)
    local result = {}
    for _, action in ipairs(Actions) do
        if not target or action.target == target then
            result[#result + 1] = action
        end
    end
    return result
end

function IsMuted(target)
    local action = Active[target]
    return action ~= nil and action.type == 'mute'
end

function IsBanned(target)
    local action = Active[target]
    return action ~= nil and action.type == 'ban'
end

RegisterNetEvent('atlas:moderationCreate')
AddEventHandler('atlas:moderationCreate', function(target, actionType, reason, duration)
    CreateAction(source, target, actionType, reason, duration)
end)

RegisterNetEvent('atlas:moderationClear')
AddEventHandler('atlas:moderationClear', function(target)
    ClearAction(source, target)
end)

RegisterCommand('admin:warn', function(source, _, args)
    CreateAction(source, tonumber(args[1]), 'warn', table.concat(args, ' ', 2))
end)

exports('CreateAction', CreateAction)
exports('ClearAction', ClearAction)
exports('GetActiveAction', GetActiveAction)
exports('IsMuted', IsMuted)
exports('IsBanned', IsBanned)
