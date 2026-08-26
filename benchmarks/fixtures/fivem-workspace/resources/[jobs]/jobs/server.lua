function LoadCharacter(source)
    local player = exports.core:GetPlayer(source)
    exports.inventory:AddItem(source, 'water', 1)
    lib.callback.await('inventory:get', false)
    return player
end

function repairVehicle(vehicle)
    TriggerEvent('vehicle:repair', vehicle)
    return vehicle
end

RegisterNetEvent('jobs:start')
AddEventHandler('jobs:start', function()
    return LoadCharacter(source)
end)

RegisterCommand('jobs:start', function(source)
    return LoadCharacter(source)
end)

exports('StartJob', function(source)
    return LoadCharacter(source)
end)
