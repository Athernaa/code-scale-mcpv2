CoreConfig = {
    frameworkName = 'atlas',
    maxCharacters = 4,
    defaultCash = 250,
    defaultBank = 1000,
    respawn = { x = 215.2, y = -810.1, z = 30.7 },
    jobs = { 'unemployed', 'mechanic', 'police', 'medic' },
    inventory = { maxWeight = 120, slotCount = 40 },
}

function CoreConfig.hasJob(job)
    for _, value in ipairs(CoreConfig.jobs) do
        if value == job then
            return true
        end
    end
    return false
end
