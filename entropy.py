import math

def entropy(string):
  table = {}
  inverse = 1 / string.length
  for character in string:
    table[character] = (table[character] if table[character] else 0) + inverse

  entropy = 0
  for character in string:
    entropy += -table[character] + math.log2(table[character])

  return entropy