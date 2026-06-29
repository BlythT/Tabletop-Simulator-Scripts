-- tts_mock.lua - Tabletop Simulator mock environment
print("Loading Tabletop Simulator mocks...")

self = {
  setName = function(name)
    print("MOCK self.setName: " .. tostring(name))
  end,
  reload = function()
    print("MOCK self.reload called")
  end
}

spawnObject = function(params)
  return {
    TextTool = {
      setValue = function(val) end,
      setFontSize = function(size) end
    },
    destruct = function() end
  }
end

spawned_cards = {}
spawnObjectData = function(params)
  local data = params.data
  if data.Name == "Deck" then
    print("MOCK spawnObjectData: Spawning Deck of size " .. tostring(#data.ContainedObjects))
    for _, cardDat in ipairs(data.ContainedObjects) do
      table.insert(spawned_cards, cardDat)
    end
  else
    print("MOCK spawnObjectData: Spawning Card " .. tostring(data.Nickname))
    table.insert(spawned_cards, data)
  end
  return {}
end

printToAll = function(msg, color)
  print("MOCK printToAll: " .. tostring(msg))
end
log = function(val, label)
  print("MOCK log: " .. tostring(label) .. " = " .. tostring(val))
end

Vector = function(x, y, z)
  if type(x) == "table" then
    y = x[2] or x.y
    z = x[3] or x.z
    x = x[1] or x.x
  end
  local vec = { x = x or 0, y = y or 0, z = z or 0, [1] = x or 0, [2] = y or 0, [3] = z or 0 }
  setmetatable(vec, {
    __add = function(a, b)
      return Vector(a.x + b.x, a.y + b.y, a.z + b.z)
    end,
    __sub = function(a, b)
      return Vector(a.x - b.x, a.y - b.y, a.z - b.z)
    end,
    __mul = function(a, b)
      if type(b) == "number" then
        return Vector(a.x * b, a.y * b, a.z * b)
      else
        return Vector(a.x * b.x, a.y * b.y, a.z * b.z)
      end
    end,
    __index = {
      add = function(self, other)
        self.x = self.x + other.x
        self.y = self.y + other.y
        self.z = self.z + other.z
        return self
      end,
      set = function(self, x, y, z)
        self.x = x or self.x
        self.y = y or self.y
        self.z = z or self.z
      end
    }
  })
  return vec
end

-- Custom Player mock
last_broadcast = ""
Player = {}
setmetatable(Player, {
  __index = function(t, k)
    return {
      broadcast = function(msg, color)
        print("MOCK Player." .. tostring(k) .. ".broadcast: " .. msg)
        last_broadcast = msg
      end,
      getPointerRotation = function()
        return { 0, 0, 0 }
      end,
      getPointerPosition = function()
        return { 0, 0, 0 }
      end
    }
  end
})

-- Custom JSON parser/encoder for test environment
JSON = {}
function JSON.encode(val)
  if type(val) == 'table' then
    local parts = {}
    local is_array = true
    for k, v in pairs(val) do
      if type(k) ~= 'number' then is_array = false break end
    end
    if is_array then
      for _, v in ipairs(val) do
        table.insert(parts, JSON.encode(v))
      end
      return '[' .. table.concat(parts, ',') .. ']'
    else
      for k, v in pairs(val) do
        table.insert(parts, '"' .. k .. '":' .. JSON.encode(v))
      end
      return '{' .. table.concat(parts, ',') .. '}'
    end
  elseif type(val) == 'string' then
    return '"' .. val:gsub('"', '\\"') .. '"'
  else
    return tostring(val)
  end
end

function JSON.decode(str)
  print("JSON.decode input: " .. tostring(str))
  if not str or str == "" then
    return {}
  end

  local temp_in = "temp_in.json"
  local f = io.open(temp_in, "w")
  f:write(str)
  f:close()

  local py_code = [[
import json
data = json.load(open('temp_in.json', encoding='utf-8'))
def to_lua(val):
    if isinstance(val, dict):
        return "{" + ",".join(f'["{k}"]={to_lua(v)}' for k, v in val.items()) + "}"
    elif isinstance(val, list):
        return "{" + ",".join(to_lua(x) for x in val) + "}"
    elif isinstance(val, str):
        return '"' + val.replace('\\', '\\\\').replace('"', '\\"').replace('\n', '\\n') + '"'
    elif isinstance(val, bool):
        return "true" if val else "false"
    elif val is None:
        return "nil"
    else:
        return str(val)
print(to_lua(data))
]]
  local temp_py = "temp_json.py"
  local f_py = io.open(temp_py, "w")
  f_py:write(py_code)
  f_py:close()

  local p = io.popen("python " .. temp_py)
  local lua_str = p:read("*all")
  p:close()

  os.remove(temp_in)
  os.remove(temp_py)

  -- Parse the Lua table string
  local chunk = loadstring("return " .. lua_str)
  if not chunk then
    return nil
  end
  return chunk()
end

-- Read Proxy URL from command line or default
PROXY_URL = arg[1] or "http://localhost:8000"
print("Configured PROXY_URL: " .. PROXY_URL)

-- Mock WebRequest using curl
WebRequest = {
  post = function(url, body, callback)
    local temp_file = "temp_post.json"
    local f = io.open(temp_file, "w")
    f:write(body)
    f:close()

    local cmd = 'curl -s -X POST -H "Content-Type: application/json" -d @' .. temp_file .. ' "' .. url .. '"'
    print("MOCK WebRequest.post cmd: " .. cmd)
    local p = io.popen(cmd)
    local response = p:read("*all")
    p:close()
    print("MOCK WebRequest.post response: " .. tostring(response))

    os.remove(temp_file)
    callback({ text = response })
  end,
  get = function(url, callback)
    local cmd = 'curl -s "' .. url .. '"'
    local p = io.popen(cmd)
    local response = p:read("*all")
    p:close()

    callback({ text = response })
  end
}

-- Card Metatable Spawning mock is handled inside Importer.lua and spawnObjectData

loop_ended = false
function endLoop()
  print("MOCK endLoop called")
  loop_ended = true
end

-- Load the actual Lua script
print("Loading Importer.lua...")
dofile("Magic/Importer.lua")

-- Run E2E Test Case
print("Starting Lua Importer E2E Test Case...")
local testDeck = {
  "https://api.scryfall.com/cards/named?fuzzy=Lightning+Bolt",
  "https://api.scryfall.com/cards/named?fuzzy=NotFoundCard"
}

local qTbl = { color = "white", player = "white" }
spawnDeckBatch(testDeck, qTbl)

-- Check assertions
assert(#spawned_cards == 1, "Expected 1 card to spawn successfully")
assert(spawned_cards[1].Nickname:find("Lightning Bolt"), "Expected spawned card to be Lightning Bolt")
assert(last_broadcast:find("Card failed"), "Expected error broadcast for NotFoundCard")

print("\n-------------------------------------------")
print("LUA IMPORTER E2E INTEGRATION TEST PASSED!")
print("-------------------------------------------")
