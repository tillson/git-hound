import math

def entropy(string):
  if len(string) == 0:
    return 0
  table = {}
  inverse = 1 / len(string)
  for character in string:
    table[character] = (table[character] if table.get(character) else 0) + inverse

  entropy = 0
  for character in string:
    entropy += -table[character] * math.log2(table[character])

  return entropy
