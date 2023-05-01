print("Hello World!")
local m = require("CallGo")
local key = require("SuKey")
local scr = require("SuScreen")
scr.CaptureScreen("sl/s",{0,0,1920,1080})
print(scr.SaveBitmap("sl/s",".png"))
print(scr.ReadBitmap("sl/s1"))
--SuBitmap["sl/s3"]=SuBitmap["sl/s1"]
print(scr.SaveBitmap("sl/s3",".png"))
scr.ReadAllBitmap("sl")
--[[key.KeyTap({"esc", "ctrl", "shift"})
key.TypeStr("0000000")
m.myfunc()
print(m.name)
print(m.showalert("AreU","Quit"))
]]
print(m.showalert("AreU","Quit"))
function max(num1, num2)

   if (num1 > num2) then
      result = num1;
   else
      result = num2;
   end

   return result;
end
-- 调用函数
print("两值比较最大值为 ",max(10,4))
print("两值比较最大值为 ",max(5,6))