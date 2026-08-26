HousingConfig = {
    defaultInterior = 'modern',
    accessRadius = 2.5,
    interiors = { modern = 1, classic = 2, penthouse = 3 },
}

function HousingConfig.validInterior(name)
    return HousingConfig.interiors[name] ~= nil
end
