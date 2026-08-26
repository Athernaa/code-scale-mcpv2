function openInventory()
    TriggerServerEvent('inventory:use')
    TriggerServerEvent('jobs:start')
end

RegisterNetEvent('jobs:complete')
AddEventHandler('jobs:complete', function()
    return true
end)
