MAX = 2**256 - 1
diff = 2566029542
target = MAX // diff
print(f'Target: {hex(target)}')
print(f'Target length in bits: {target.bit_length()}')
