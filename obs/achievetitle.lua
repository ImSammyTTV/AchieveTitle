--[[
AchieveTitle for OBS Studio (Windows, macOS, Linux)

Install: OBS -> Tools -> Scripts -> "+" -> pick this file.

- Starts AchieveTitle together with OBS
- Turns automatic title updates on when you go live and off when you stop
  (your original title is restored if that option is on in AchieveTitle)
- Add the control panel: View -> Docks -> Custom Browser Docks,
  name "AchieveTitle", URL http://localhost:7878/dock
]]

obs = obslua

local exe_path = ""
local port = 7878
local launch_with_obs = true
local follow_stream = true
local quit_with_obs = false

local is_windows = package.config:sub(1, 1) == "\\"
local is_mac = false
if not is_windows then
  local f = io.popen("uname -s")
  if f then is_mac = f:read("*l") == "Darwin"; f:close() end
end

local function sh_quote(s)
  if is_windows then return '"' .. s .. '"' end
  return "'" .. s:gsub("'", "'\\''") .. "'"
end

-- Talks to the local AchieveTitle app. curl ships with Windows 10+, macOS and most Linux distros.
local function api(path, body)
  local base = "http://localhost:" .. port
  local cmd = "curl -s -m 2 -X POST -H " .. sh_quote("Origin: " .. base)
  if body then
    cmd = cmd .. " -H " .. sh_quote("Content-Type: application/json") .. " --data " .. sh_quote(body)
  end
  cmd = cmd .. " " .. base .. path
  if is_windows then
    os.execute("start \"\" /B " .. cmd .. " >NUL 2>&1")
  else
    os.execute(cmd .. " >/dev/null 2>&1 &")
  end
end

local function launch()
  if exe_path == "" then
    obs.script_log(obs.LOG_WARNING, "AchieveTitle: set the path to the AchieveTitle app in the script settings")
    return
  end
  -- If it's already running, the app notices the busy port and exits by itself.
  local args = " -no-browser -port " .. port
  if is_windows then
    os.execute("start \"AchieveTitle\" /MIN " .. sh_quote(exe_path) .. args)
  elseif is_mac and exe_path:match("%.app/?$") then
    os.execute("open -g -a " .. sh_quote(exe_path) .. " --args" .. args)
  else
    os.execute(sh_quote(exe_path) .. args .. " >/dev/null 2>&1 &")
  end
  obs.script_log(obs.LOG_INFO, "AchieveTitle: started " .. exe_path)
end

local function on_event(event)
  if not follow_stream then return end
  if event == obs.OBS_FRONTEND_EVENT_STREAMING_STARTED then
    api("/api/enabled", '{"enabled":true}')
  elseif event == obs.OBS_FRONTEND_EVENT_STREAMING_STOPPED then
    api("/api/enabled", '{"enabled":false}')
  end
end

function script_description()
  return [[<h3>AchieveTitle</h3>
<p>Shows your Steam achievement progress in your Twitch title.</p>
<p>Control panel: <b>View → Docks → Custom Browser Docks</b>, URL <code>http://localhost:7878/dock</code></p>
<p><a href="https://github.com/ImSammyTTV/AchieveTitle">Download &amp; help</a></p>]]
end

function script_properties()
  local p = obs.obs_properties_create()
  obs.obs_properties_add_path(p, "exe_path", "AchieveTitle app", obs.OBS_PATH_FILE, "", nil)
  obs.obs_properties_add_bool(p, "launch_with_obs", "Start AchieveTitle with OBS")
  obs.obs_properties_add_bool(p, "follow_stream", "Update title only while I'm live")
  obs.obs_properties_add_bool(p, "quit_with_obs", "Quit AchieveTitle when OBS closes")
  obs.obs_properties_add_int(p, "port", "Port", 1024, 65535, 1)
  obs.obs_properties_add_button(p, "open_settings", "Open AchieveTitle settings", function()
    local url = "http://localhost:" .. port
    if is_windows then os.execute("start \"\" " .. url)
    elseif is_mac then os.execute("open " .. url)
    else os.execute("xdg-open " .. url .. " >/dev/null 2>&1 &") end
    return false
  end)
  return p
end

function script_defaults(s)
  obs.obs_data_set_default_bool(s, "launch_with_obs", true)
  obs.obs_data_set_default_bool(s, "follow_stream", true)
  obs.obs_data_set_default_bool(s, "quit_with_obs", false)
  obs.obs_data_set_default_int(s, "port", 7878)
end

function script_update(s)
  exe_path = obs.obs_data_get_string(s, "exe_path")
  launch_with_obs = obs.obs_data_get_bool(s, "launch_with_obs")
  follow_stream = obs.obs_data_get_bool(s, "follow_stream")
  quit_with_obs = obs.obs_data_get_bool(s, "quit_with_obs")
  port = obs.obs_data_get_int(s, "port")
end

function script_load(s)
  script_update(s)
  if launch_with_obs then launch() end
  obs.obs_frontend_add_event_callback(on_event)
end

function script_unload()
  if quit_with_obs then api("/api/quit") end
end
