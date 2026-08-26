VehicleConfig = {
    serviceDistance = 6.0,
    repairCost = 350,
    classes = { compact = 0, sedan = 1, suv = 2, service = 18 },
}

function VehicleConfig.isTracked(class)
    return class ~= nil and class ~= 13 and class ~= 14 and class ~= 15 and class ~= 16
end
