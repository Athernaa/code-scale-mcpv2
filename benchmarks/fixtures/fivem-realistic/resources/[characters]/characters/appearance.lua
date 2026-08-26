local Appearance = {}
local Presets = {
    citizen = { model = 'mp_m_freemode_01', blend = { mother = 0, father = 0 }, clothes = {} },
    worker = { model = 'mp_m_freemode_01', blend = { mother = 1, father = 0 }, clothes = { shirt = 15, pants = 5 } },
    medic = { model = 's_m_m_doctor_01', blend = {}, clothes = { shirt = 1, pants = 1 } },
}

local function ensureAppearance(source)
    if not Appearance[source] then
        Appearance[source] = SharedUtils.copy(Presets.citizen)
    end
    return Appearance[source]
end

function GetAppearance(source)
    return SharedUtils.copy(ensureAppearance(source))
end

function SetAppearance(source, value)
    if not value or not value.model then
        return false, 'appearance_invalid'
    end
    Appearance[source] = SharedUtils.copy(value)
    Appearance[source].version = CharacterConfig.appearanceVersion
    TriggerClientEvent('atlas:appearanceChanged', source, Appearance[source])
    return true
end

function ApplyPreset(source, name)
    local preset = Presets[name]
    if not preset then
        return false, 'preset_missing'
    end
    return SetAppearance(source, preset)
end

function SaveAppearance(source)
    local character = exports.characters:GetCharacter(source)
    if not character then
        return false, 'character_missing'
    end
    character.appearance = GetAppearance(source)
    return exports.characters:SaveCharacter(source, character)
end

function ResetAppearance(source)
    Appearance[source] = SharedUtils.copy(Presets.citizen)
    TriggerClientEvent('atlas:appearanceChanged', source, Appearance[source])
    return true
end

RegisterNetEvent('atlas:appearanceSet')
AddEventHandler('atlas:appearanceSet', function(value)
    SetAppearance(source, value)
end)

RegisterNetEvent('atlas:appearancePreset')
AddEventHandler('atlas:appearancePreset', function(name)
    ApplyPreset(source, name)
end)

RegisterNetEvent('atlas:appearanceSave')
AddEventHandler('atlas:appearanceSave', function()
    SaveAppearance(source)
end)

RegisterCommand('character:preset', function(source, _, args)
    ApplyPreset(source, args[1] or 'citizen')
end)

exports('GetAppearance', GetAppearance)
exports('SetAppearance', SetAppearance)
exports('ApplyPreset', ApplyPreset)
exports('SaveAppearance', SaveAppearance)
