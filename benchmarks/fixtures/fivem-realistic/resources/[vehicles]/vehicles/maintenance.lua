local Maintenance = {}
local Services = {}
local ServiceCatalog = {
    oil = { duration = 5, cost = 100, damage = 0.1 },
    tires = { duration = 8, cost = 220, damage = 0.25 },
    engine = { duration = 15, cost = 600, damage = 0.5 },
}

local function serviceKey(source, plate)
    return tostring(source) .. ':' .. tostring(plate)
end

function GetService(name)
    return ServiceCatalog[name]
end

function EstimateService(plate, name)
    local vehicle = GetVehicle(plate)
    local service = ServiceCatalog[name]
    if not vehicle or not service then
        return false, 'service_missing'
    end
    local multiplier = 1 + (vehicle.damage / 100)
    return { plate = plate, name = name, cost = math.floor(service.cost * multiplier), duration = service.duration }
end

function StartService(source, plate, name)
    local estimate, errorMessage = EstimateService(plate, name)
    if not estimate then
        return false, errorMessage
    end
    if Services[serviceKey(source, plate)] then
        return false, 'service_in_progress'
    end
    if not exports.core:RemoveCash(source, estimate.cost) then
        return false, 'insufficient_funds'
    end
    Services[serviceKey(source, plate)] = { estimate = estimate, started = os.time() }
    TriggerClientEvent('atlas:serviceStarted', source, estimate)
    return true, estimate
end

function FinishService(source, plate)
    local key = serviceKey(source, plate)
    local active = Services[key]
    if not active then
        return false, 'service_missing'
    end
    local vehicle = GetVehicle(plate)
    if not vehicle then
        Services[key] = nil
        return false, 'vehicle_missing'
    end
    local service = ServiceCatalog[active.estimate.name]
    vehicle.damage = math.max(0, vehicle.damage - service.damage * 100)
    Services[key] = nil
    TriggerClientEvent('atlas:serviceFinished', source, vehicle, active.estimate)
    return true, vehicle
end

function CancelService(source, plate)
    local key = serviceKey(source, plate)
    if not Services[key] then
        return false, 'service_missing'
    end
    Services[key] = nil
    return true
end

RegisterNetEvent('atlas:serviceStart')
AddEventHandler('atlas:serviceStart', function(plate, name)
    StartService(source, plate, name)
end)

RegisterNetEvent('atlas:serviceFinish')
AddEventHandler('atlas:serviceFinish', function(plate)
    FinishService(source, plate)
end)

RegisterNetEvent('atlas:serviceCancel')
AddEventHandler('atlas:serviceCancel', function(plate)
    CancelService(source, plate)
end)

exports('EstimateService', EstimateService)
exports('StartService', StartService)
exports('FinishService', FinishService)
