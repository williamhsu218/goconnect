on run arguments
    set commandText to "/usr/bin/python3"
    repeat with value in arguments
        set commandText to commandText & " " & quoted form of (value as text)
    end repeat
    return do shell script commandText with administrator privileges
end run
