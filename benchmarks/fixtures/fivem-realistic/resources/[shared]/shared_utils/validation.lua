Validation = {}

function Validation.required(value, name)
    if value == nil or value == '' then
        return false, (name or 'value') .. '_required'
    end
    return true
end

function Validation.string(value, name, minimum, maximum)
    if type(value) ~= 'string' then
        return false, (name or 'value') .. '_string'
    end
    if minimum and #value < minimum then
        return false, (name or 'value') .. '_short'
    end
    if maximum and #value > maximum then
        return false, (name or 'value') .. '_long'
    end
    return true
end

function Validation.number(value, name, minimum, maximum)
    if type(value) ~= 'number' then
        return false, (name or 'value') .. '_number'
    end
    if minimum and value < minimum then
        return false, (name or 'value') .. '_low'
    end
    if maximum and value > maximum then
        return false, (name or 'value') .. '_high'
    end
    return true
end

function Validation.oneOf(value, values, name)
    if not SharedUtils.listContains(values, value) then
        return false, (name or 'value') .. '_invalid'
    end
    return true
end

function Validation.all(checks)
    local errors = {}
    for _, check in ipairs(checks or {}) do
        local ok, errorMessage = check()
        if not ok then
            errors[#errors + 1] = errorMessage
        end
    end
    return #errors == 0, errors
end

function Validation.character(character)
    return Validation.all({
        function() return Validation.required(character and character.id, 'character_id') end,
        function() return Validation.string(character and character.name, 'character_name', 2, 32) end,
    })
end

function Validation.item(item, count)
    return Validation.all({
        function() return Validation.string(item, 'item', 1, 40) end,
        function() return Validation.number(count, 'count', 1, 100) end,
    })
end

function Validation.property(propertyId, interior)
    return Validation.all({
        function() return Validation.required(propertyId, 'property_id') end,
        function() return Validation.oneOf(interior, { 'modern', 'classic', 'penthouse' }, 'interior') end,
    })
end

return Validation
