Set fso = CreateObject("Scripting.FileSystemObject")
baseDir = fso.GetParentFolderName(WScript.ScriptFullName)
scriptPath = fso.BuildPath(baseDir, "scripts\Check-MantisNewItems.ps1")

cmd = "powershell -NoProfile -ExecutionPolicy Bypass -File " & Chr(34) & scriptPath & Chr(34)

Set shell = CreateObject("WScript.Shell")
shell.CurrentDirectory = baseDir
shell.Run cmd, 0, False
