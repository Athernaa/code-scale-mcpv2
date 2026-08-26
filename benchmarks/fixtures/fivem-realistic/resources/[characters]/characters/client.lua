local loaded = nil
local loading = false

function GetLoadedCharacter()
    return loaded
end

function RequestCharacter(characterId)
    if loading then
        return false
    end
    loading = true
    TriggerServerEvent('atlas:characterLoad', characterId)
    return true
end

RegisterNetEvent('atlas:characterLoaded')
AddEventHandler('atlas:characterLoaded', function(character)
    loaded = character
    loading = false
    TriggerEvent('atlas:notify', 'Character loaded', 'success')
end)

RegisterNetEvent('atlas:characterLoadWarning')
AddEventHandler('atlas:characterLoadWarning', function(reason)
    loading = false
    TriggerEvent('atlas:notify', 'Inventory warning: ' .. tostring(reason), 'warning')
end)

RegisterCommand('character:select', function(_, args)
    RequestCharacter(args[1])
end)

CreateThread(function()
    Wait(500)
    RequestCharacter()
end)
