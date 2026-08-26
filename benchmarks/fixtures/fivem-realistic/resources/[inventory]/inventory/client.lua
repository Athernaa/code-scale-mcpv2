local open = false
local lastVersion = 0

local function setOpen(value)
    open = value
    SetNuiFocus(value, value)
    SendNUIMessage({ action = value and 'inventoryOpen' or 'inventoryClose' })
end

function IsInventoryOpen()
    return open
end

function RequestInventory()
    TriggerServerEvent('atlas:inventoryRequest')
end

RegisterNetEvent('atlas:inventoryChanged')
AddEventHandler('atlas:inventoryChanged', function(version)
    lastVersion = version
    SendNUIMessage({ action = 'inventoryVersion', version = version })
end)

RegisterNetEvent('atlas:inventorySnapshot')
AddEventHandler('atlas:inventorySnapshot', function(inventory)
    SendNUIMessage({ action = 'inventorySnapshot', inventory = inventory })
end)

RegisterNUICallback('inventory:close', function(_, callback)
    setOpen(false)
    callback({ ok = true })
end)

RegisterNUICallback('inventory:move', function(data, callback)
    TriggerServerEvent('atlas:inventoryMove', data.from, data.to)
    callback({ ok = true, version = lastVersion })
end)

RegisterCommand('inventory:open', function()
    setOpen(not open)
    if open then
        RequestInventory()
    end
end)

RegisterKeyMapping('inventory:open', 'Open inventory', 'keyboard', 'I')
