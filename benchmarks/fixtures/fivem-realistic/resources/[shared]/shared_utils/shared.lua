SharedUtils = {}

function SharedUtils.trim(value)
    if value == nil then
        return ''
    end
    return (string.gsub(value, '^%s*(.-)%s*$', '%1'))
end

function SharedUtils.copy(source)
    local result = {}
    for key, value in pairs(source or {}) do
        if type(value) == 'table' then
            result[key] = SharedUtils.copy(value)
        else
            result[key] = value
        end
    end
    return result
end

function SharedUtils.merge(base, overlay)
    local result = SharedUtils.copy(base)
    for key, value in pairs(overlay or {}) do
        if type(value) == 'table' and type(result[key]) == 'table' then
            result[key] = SharedUtils.merge(result[key], value)
        else
            result[key] = value
        end
    end
    return result
end

function SharedUtils.clamp(value, lower, upper)
    if value < lower then
        return lower
    end
    if value > upper then
        return upper
    end
    return value
end

function SharedUtils.round(value, places)
    local scale = 10 ^ (places or 0)
    return math.floor(value * scale + 0.5) / scale
end

function SharedUtils.isBlank(value)
    return value == nil or SharedUtils.trim(tostring(value)) == ''
end

function SharedUtils.toInteger(value, fallback)
    local number = tonumber(value)
    if not number then
        return fallback or 0
    end
    return math.floor(number)
end

function SharedUtils.toBoolean(value)
    if value == true or value == 1 or value == '1' or value == 'true' then
        return true
    end
    return false
end

function SharedUtils.listContains(values, wanted)
    for _, value in ipairs(values or {}) do
        if value == wanted then
            return true
        end
    end
    return false
end

function SharedUtils.indexBy(values, key)
    local result = {}
    for _, value in ipairs(values or {}) do
        if value[key] ~= nil then
            result[value[key]] = value
        end
    end
    return result
end

function SharedUtils.safeCall(callback, ...)
    if type(callback) ~= 'function' then
        return false, 'callback_missing'
    end
    local ok, value = pcall(callback, ...)
    if not ok then
        return false, value
    end
    return true, value
end

function SharedUtils.newId(prefix, value)
    local suffix = tostring(value or os.time())
    return string.format('%s:%s', prefix or 'id', suffix)
end

return SharedUtils
