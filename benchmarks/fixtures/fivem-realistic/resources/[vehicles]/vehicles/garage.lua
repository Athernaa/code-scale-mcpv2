local Garages = {}

local function ensureGarage(name)
    if not Garages[name] then
        Garages[name] = { name = name, capacity = 20, vehicles = {}, open = true }
    end
    return Garages[name]
end

function CreateGarage(name, capacity)
    local garage = ensureGarage(name)
    garage.capacity = capacity or garage.capacity
    return garage
end

function AddGarageVehicle(name, plate)
    local garage = ensureGarage(name)
    if #garage.vehicles >= garage.capacity then
        return false, 'garage_full'
    end
    for _, value in ipairs(garage.vehicles) do
        if value == plate then
            return false, 'vehicle_exists'
        end
    end
    garage.vehicles[#garage.vehicles + 1] = plate
    return true
end

function RemoveGarageVehicle(name, plate)
    local garage = Garages[name]
    if not garage then
        return false, 'garage_missing'
    end
    for index, value in ipairs(garage.vehicles) do
        if value == plate then
            table.remove(garage.vehicles, index)
            return true
        end
    end
    return false, 'vehicle_missing'
end

function ListGarageVehicles(name)
    local garage = Garages[name]
    return garage and SharedUtils.copy(garage.vehicles) or {}
end

function SetGarageOpen(name, open)
    local garage = ensureGarage(name)
    garage.open = open == true
    return garage.open
end

function CanUseGarage(name)
    local garage = Garages[name]
    return garage ~= nil and garage.open == true
end

RegisterNetEvent('atlas:garageList')
AddEventHandler('atlas:garageList', function(name)
    TriggerClientEvent('atlas:garageListResult', source, name, ListGarageVehicles(name))
end)

RegisterCommand('garage:list', function(source, _, args)
    TriggerClientEvent('atlas:garageListResult', source, args[1], ListGarageVehicles(args[1]))
end)

exports('CreateGarage', CreateGarage)
exports('AddGarageVehicle', AddGarageVehicle)
exports('ListGarageVehicles', ListGarageVehicles)
