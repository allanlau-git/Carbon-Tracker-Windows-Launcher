#!/usr/bin/env python3
import argparse, struct, pathlib, shutil

def align(v,a): return (v + a - 1) // a * a

def parse_ico(path):
    data=pathlib.Path(path).read_bytes()
    reserved, typ, count=struct.unpack_from('<HHH',data,0)
    if reserved!=0 or typ!=1 or count<1: raise ValueError('Not a valid ICO file')
    images=[]
    group_entries=[]
    for i in range(count):
        off=6+i*16
        w,h,colors,res,planes,bpp,size,img_off=struct.unpack_from('<BBBBHHII',data,off)
        img=data[img_off:img_off+size]
        if len(img)!=size: raise ValueError('Truncated ICO image')
        images.append(img)
        group_entries.append((w,h,colors,res,planes,bpp,size,i+1))
    group=bytearray(struct.pack('<HHH',0,1,count))
    for e in group_entries:
        group += struct.pack('<BBBBHHIH',*e)
    return images, bytes(group)

class RsrcBuilder:
    def __init__(self, base_rva):
        self.b=bytearray()
        self.base_rva=base_rva
    def reserve(self,n,align_to=4):
        while len(self.b)%align_to: self.b.append(0)
        o=len(self.b); self.b.extend(b'\0'*n); return o
    def add_data(self,data,align_to=4):
        o=self.reserve(0,align_to); self.b.extend(data); return o
    def put_dir(self, off, entries):
        struct.pack_into('<IIHHHH', self.b, off, 0,0,0,0,0,len(entries))
        eoff=off+16
        for rid,target,isdir in entries:
            struct.pack_into('<II',self.b,eoff,rid,(0x80000000 if isdir else 0)|target); eoff+=8
    def put_data_entry(self, off, data_off, size):
        struct.pack_into('<IIII',self.b,off,self.base_rva+data_off,size,0,0)

def build_rsrc(ico_path, base_rva):
    icons, group=parse_ico(ico_path)
    rb=RsrcBuilder(base_rva)
    root=rb.reserve(16+2*8)
    icon_type=rb.reserve(16+len(icons)*8)
    group_type=rb.reserve(16+1*8)
    icon_lang_dirs=[]
    for _ in icons: icon_lang_dirs.append(rb.reserve(16+1*8))
    group_lang=rb.reserve(16+1*8)
    icon_data_entries=[rb.reserve(16) for _ in icons]
    group_data_entry=rb.reserve(16)
    icon_data_offs=[rb.add_data(img,4) for img in icons]
    group_data_off=rb.add_data(group,4)
    rb.put_dir(root,[(3,icon_type,True),(14,group_type,True)])
    rb.put_dir(icon_type,[(i+1,icon_lang_dirs[i],True) for i in range(len(icons))])
    rb.put_dir(group_type,[(1,group_lang,True)])
    for i,d in enumerate(icon_lang_dirs): rb.put_dir(d,[(1033,icon_data_entries[i],False)])
    rb.put_dir(group_lang,[(1033,group_data_entry,False)])
    for i,de in enumerate(icon_data_entries): rb.put_data_entry(de,icon_data_offs[i],len(icons[i]))
    rb.put_data_entry(group_data_entry,group_data_off,len(group))
    return bytes(rb.b)

def patch_pe(exe, ico, out):
    data=bytearray(pathlib.Path(exe).read_bytes())
    if data[:2]!=b'MZ': raise ValueError('Not PE/MZ')
    pe=struct.unpack_from('<I',data,0x3c)[0]
    if data[pe:pe+4]!=b'PE\0\0': raise ValueError('Invalid PE signature')
    coff=pe+4
    nsec=struct.unpack_from('<H',data,coff+2)[0]
    opt_size=struct.unpack_from('<H',data,coff+16)[0]
    opt=coff+20
    if struct.unpack_from('<H',data,opt)[0]!=0x20b: raise ValueError('Only PE32+ supported')
    sec_align=struct.unpack_from('<I',data,opt+32)[0]
    file_align=struct.unpack_from('<I',data,opt+36)[0]
    size_headers=struct.unpack_from('<I',data,opt+60)[0]
    sec_table=opt+opt_size
    new_hdr=sec_table+nsec*40
    if new_hdr+40 > size_headers: raise ValueError('No room for another PE section header')
    max_end=0
    first_raw=None
    for i in range(nsec):
        s=sec_table+i*40
        vsize, va, rawsize, rawptr=struct.unpack_from('<IIII',data,s+8)
        max_end=max(max_end, va+max(vsize,rawsize))
        if rawptr and (first_raw is None or rawptr<first_raw): first_raw=rawptr
    new_rva=align(max_end,sec_align)
    rsrc=build_rsrc(ico,new_rva)
    rawptr=align(len(data),file_align)
    rawsize=align(len(rsrc),file_align)
    if len(data)<rawptr: data.extend(b'\0'*(rawptr-len(data)))
    data.extend(rsrc)
    data.extend(b'\0'*(rawsize-len(rsrc)))
    name=b'.rsrc\0\0\0'
    chars=0x40000040  # initialized data | read
    data[new_hdr:new_hdr+40]=struct.pack('<8sIIIIIIHHI',name,len(rsrc),new_rva,rawsize,rawptr,0,0,0,0,chars)
    struct.pack_into('<H',data,coff+2,nsec+1)
    struct.pack_into('<I',data,opt+56,align(new_rva+len(rsrc),sec_align))
    # resource data directory entry #2
    dd=opt+112+2*8
    struct.pack_into('<II',data,dd,new_rva,len(rsrc))
    pathlib.Path(out).write_bytes(data)

if __name__=='__main__':
    ap=argparse.ArgumentParser()
    ap.add_argument('exe'); ap.add_argument('ico'); ap.add_argument('-o','--out',required=True)
    a=ap.parse_args(); patch_pe(a.exe,a.ico,a.out)