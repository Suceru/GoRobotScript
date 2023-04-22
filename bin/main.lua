print("Hello World!")
local m = require("CallGo")
local key = require("SuKey")
key.TypeStr("0000000")
m.myfunc()
print(m.name)
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