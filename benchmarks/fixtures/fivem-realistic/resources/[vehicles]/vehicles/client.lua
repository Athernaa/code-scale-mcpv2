local activeVehicle = nil
local vehicleState = { fuel = 100, damage = 0 }

function SetActiveVehicle(vehicle)
    activeVehicle = vehicle
    vehicleState = { fuel = 100, damage = 0 }
end

function GetActiveVehicle()
    return activeVehicle
end

function StoreActiveVehicle()
    if not activeVehicle then
        return false
    end
    TriggerServerEvent('atlas:vehicleStore', activeVehicle.plate, vehicleState.fuel, vehicleState.damage)
    return true
end

RegisterNetEvent('atlas:vehicleSpawned')
AddEventHandler('atlas:vehicleSpawned', function(vehicle)
    SetActiveVehicle(vehicle)
end)

RegisterNetEvent('atlas:vehicleStored')
AddEventHandler('atlas:vehicleStored', function()
    activeVehicle = nil
end)

RegisterNetEvent('atlas:vehicleRepaired')
AddEventHandler('atlas:vehicleRepaired', function()
    vehicleState.damage = 0
end)

RegisterCommand('vehicle:store', function()
    StoreActiveVehicle()
end)

RegisterCommand('vehicle:repair', function()
    if activeVehicle then
        TriggerServerEvent('atlas:vehicleRepair', activeVehicle.plate)
    end
end)

CreateThread(function()
    while true do
        Wait(1000)
        if activeVehicle then
            vehicleState.fuel = math.max(0, vehicleState.fuel - 0.1)
        end
    end
end)
