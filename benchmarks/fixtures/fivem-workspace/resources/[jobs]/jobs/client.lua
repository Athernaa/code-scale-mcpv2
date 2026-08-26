function openInventory()
    TriggerServerEvent('inventory:use')
    TriggerServerEvent('jobs:start')
    return lib.callback.await('inventory:get', false)
end

RegisterNetEvent('jobs:complete')
AddEventHandler('jobs:complete', function()
    return true
end)
