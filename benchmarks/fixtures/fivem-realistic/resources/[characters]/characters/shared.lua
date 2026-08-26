CharacterConfig = {
    spawn = { x = 215.2, y = -810.1, z = 30.7, heading = 157.0 },
    appearanceVersion = 3,
    maxNameLength = 32,
}

function CharacterConfig.validName(name)
    return type(name) == 'string' and #name >= 2 and #name <= CharacterConfig.maxNameLength
end
