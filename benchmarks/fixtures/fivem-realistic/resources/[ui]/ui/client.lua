local panels = { inventory = false, jobs = false, housing = false }

local function setPanel(name, visible)
    panels[name] = visible
    SendNUIMessage({ action = 'panel', name = name, visible = visible })
end

function TogglePanel(name)
    if panels[name] == nil then
        return false
    end
    setPanel(name, not panels[name])
    return panels[name]
end

function ClosePanels()
    for name in pairs(panels) do
        setPanel(name, false)
    end
end

RegisterNUICallback('ui:close', function(_, callback)
    ClosePanels()
    SetNuiFocus(false, false)
    callback({ ok = true })
end)

RegisterNUICallback('ui:openInventory', function(_, callback)
    TogglePanel('inventory')
    TriggerEvent('atlas:inventoryRequest')
    callback({ ok = true })
end)

RegisterNUICallback('ui:openJobs', function(_, callback)
    TogglePanel('jobs')
    callback({ ok = true })
end)

RegisterNUICallback('ui:openHousing', function(_, callback)
    TogglePanel('housing')
    callback({ ok = true })
end)

RegisterCommand('ui:close', ClosePanels)
