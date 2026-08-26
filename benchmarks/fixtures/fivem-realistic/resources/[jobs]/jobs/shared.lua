JobConfig = {
    mechanic = { salary = 180, duty = true },
    police = { salary = 240, duty = true },
    medic = { salary = 220, duty = true },
    unemployed = { salary = 50, duty = false },
}

function JobConfig.exists(job)
    return JobConfig[job] ~= nil
end
