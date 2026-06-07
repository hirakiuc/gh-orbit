BEGIN {
	FS = ": "
}

function escape_md(value) {
	gsub(/\|/, "\\|", value)
	return value
}

function emit(location, name, metric, score) {
	print score "\t| " location " | " escape_md(name) " | " metric " | " score " |"
}

/^.+:[0-9]+:[0-9]+: calculated cyclomatic complexity/ {
	name = $2
	sub(/^.* function /, "", name)
	sub(/ is .*$/, "", name)

	score = $2
	sub(/^.* is /, "", score)
	sub(/,.*$/, "", score)

	emit($1, name, "cyclomatic", score)
	next
}

/^.+:[0-9]+:[0-9]+: cognitive complexity/ {
	name = $2
	sub(/^.* of func `/, "", name)
	sub(/`.*$/, "", name)

	score = $2
	sub(/^cognitive complexity /, "", score)
	sub(/ .*$/, "", score)

	emit($1, name, "cognitive", score)
	next
}

/^.+:[0-9]+:[0-9]+: Function '/ {
	name = $2
	sub(/^Function '/, "", name)
	sub(/'.*$/, "", name)

	metric = "funlen"
	if ($2 ~ /too long/) {
		metric = "too long"
	} else if ($2 ~ /too many statements/) {
		metric = "too many statements"
	}

	score = $2
	sub(/^.*\(/, "", score)
	sub(/ .*$/, "", score)

	emit($1, name, metric, score)
	next
}
